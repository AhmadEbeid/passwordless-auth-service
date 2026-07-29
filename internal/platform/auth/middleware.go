package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// SessionValidator validates an opaque bearer token and returns the principal
// it belongs to. Implemented by the auth feature's Service — this package
// never touches session storage itself.
type SessionValidator interface {
	ValidateSession(ctx context.Context, token string) (Principal, error)
}

// SessionMiddleware enforces a valid consumer session. On success the
// principal is attached to context for handlers to read via
// PrincipalFromContext.
func SessionMiddleware(validator SessionValidator) huma.Middlewares {
	return huma.Middlewares{
		func(ctx huma.Context, next func(huma.Context)) {
			token := bearerToken(ctx.Header("Authorization"))
			if token == "" {
				writeAuthError(ctx, 401, "missing session token")
				return
			}
			principal, err := validator.ValidateSession(ctx.Context(), token)
			if err != nil {
				writeAuthError(ctx, 401, "invalid or expired session")
				return
			}
			next(huma.WithContext(ctx, ContextWithPrincipal(ctx.Context(), principal)))
		},
	}
}

// AdminMiddleware enforces the coarse is-admin gate via a single configured
// shared secret. A blank expectedKey rejects every request: the gate fails
// closed.
func AdminMiddleware(expectedKey, adminID string) huma.Middlewares {
	return huma.Middlewares{
		func(ctx huma.Context, next func(huma.Context)) {
			token := bearerToken(ctx.Header("Authorization"))
			if expectedKey == "" || token == "" ||
				subtle.ConstantTimeCompare([]byte(token), []byte(expectedKey)) != 1 {
				writeAuthError(ctx, 401, "missing or invalid admin credential")
				return
			}
			next(huma.WithContext(ctx, ContextWithAdminPrincipal(ctx.Context(), AdminPrincipal{ID: adminID})))
		},
	}
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimPrefix(header, prefix)
}

// writeAuthError writes a typed error response directly (always JSON — no
// content negotiation) using the process-wide huma.NewError hook, so a
// middleware rejection carries the same stable `code` shape as a handler
// error, without requiring a huma.API reference in the middleware.
func writeAuthError(ctx huma.Context, status int, msg string) {
	err := huma.NewErrorWithContext(ctx, status, msg)
	ctx.SetHeader("Content-Type", "application/json")
	ctx.SetStatus(err.GetStatus())
	body, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		return
	}
	_, _ = ctx.BodyWriter().Write(body)
}
