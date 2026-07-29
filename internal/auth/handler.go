package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	platformauth "github.com/AhmadEbeid/passwordless-auth-service/internal/platform/auth"
	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/observability"
)

// Huma I/O structs are transport DTOs confined to this handler layer; they map
// to/from domain types at the service boundary. Domain types never leak
// through them.

type requestVerificationInput struct {
	Body struct {
		Intent          string  `json:"intent" enum:"signup,signin" doc:"Whether this starts a signup or signin flow"`
		Phone           string  `json:"phone" doc:"Egyptian mobile number in any accepted form (01…, +20…, or 10 digits)"`
		Role            string  `json:"role,omitempty" enum:"coach,athlete" doc:"Account role, set at signup and immutable (signup only)"`
		Language        string  `json:"language,omitempty" enum:"ar,en" doc:"Preferred app language (signup only)"`
		GoogleLinkToken *string `json:"google_link_token,omitempty" doc:"Signed token from /auth/google's no-account response, proving a verified Google identity to link on account creation (signup only)"`
	}
}

type requestVerificationOutput struct {
	Body struct {
		VerificationID    string    `json:"verification_id"`
		ExpiresAt         time.Time `json:"expires_at"`
		ResendAvailableAt time.Time `json:"resend_available_at"`
	}
}

type resendVerificationInput struct {
	ID string `path:"id" doc:"Verification id"`
}

type resendVerificationOutput struct {
	Body struct {
		ResendAvailableAt time.Time `json:"resend_available_at"`
	}
}

type confirmVerificationInput struct {
	ID   string `path:"id" doc:"Verification id"`
	Body struct {
		Code string `json:"code" minLength:"6" maxLength:"6" doc:"The 6-digit verification code"`
	}
}

type confirmVerificationOutput struct {
	Body struct {
		SessionToken string `json:"session_token"`
		Account      struct {
			ID       string `json:"id"`
			Role     string `json:"role"`
			Language string `json:"language"`
		} `json:"account"`
		Route string `json:"route"`
	}
}

type googleExchangeInput struct {
	Body struct {
		IDToken string `json:"id_token" doc:"Google ID token to verify"`
		Role    string `json:"role,omitempty" enum:"coach,athlete" doc:"Account role; informational only — meaningful just when this leads into a new Google signup"`
	}
}

// googleExchangeOutput is a discriminated response on one status code: the
// existing-account branch fills SessionToken/Account/Route, the no-account
// branch fills Next/PhonePrefill. Account is a pointer so it drops out of the
// no-account response instead of serializing as an all-empty object.
type googleExchangeOutput struct {
	Body struct {
		SessionToken string `json:"session_token,omitempty"`
		Account      *struct {
			ID       string `json:"id"`
			Role     string `json:"role"`
			Language string `json:"language"`
		} `json:"account,omitempty"`
		Route            string  `json:"route,omitempty"`
		Next             string  `json:"next,omitempty" doc:"Set to verify_phone when no account is linked to this Google identity yet"`
		PhonePrefill     *string `json:"phone_prefill,omitempty"`
		PendingLinkToken string  `json:"pending_link_token,omitempty" doc:"Pass this to POST /auth/verifications as google_link_token to link the verified Google identity on signup"`
	}
}

type sessionInput struct {
	Authorization string `header:"Authorization" doc:"Bearer session token"`
}

type getSessionOutput struct {
	Body struct {
		Account struct {
			ID       string `json:"id"`
			Role     string `json:"role"`
			Language string `json:"language"`
		} `json:"account"`
	}
}

// --- Admin DTOs ---

type listAccountsInput struct {
	Phone  string `query:"phone" doc:"Filter accounts to phones containing this substring"`
	Limit  int    `query:"limit" default:"50" minimum:"1" maximum:"200" doc:"Max accounts to return"`
	Offset int    `query:"offset" default:"0" minimum:"0" doc:"Accounts to skip, for pagination"`
}

type accountListItemDTO struct {
	ID            string    `json:"id"`
	Role          string    `json:"role"`
	Phone         string    `json:"phone"`
	GoogleLinked  bool      `json:"google_linked"`
	SignupMethod  string    `json:"signup_method"`
	CreatedAt     time.Time `json:"created_at"`
	LockoutActive bool      `json:"lockout_active"`
}

type listAccountsOutput struct {
	Body []accountListItemDTO
}

type clearLockoutInput struct {
	ID string `path:"id" doc:"Account id"`
}

type clearLockoutOutput struct {
	Body struct {
		Cleared bool `json:"cleared"`
	}
}

type analyticsInput struct {
	Range string `query:"range" enum:"24h,7d,30d" default:"7d" doc:"Time range for the aggregates"`
}

type analyticsOutput struct {
	Body struct {
		SignupsByMethod         map[string]int `json:"signups_by_method"`
		VerificationFailureRate float64        `json:"verification_failure_rate"`
		LockoutCount            int            `json:"lockout_count"`
	}
}

// Register mounts the auth operations on the Huma API. It is the feature's
// registration seam: the composition root builds the Service and calls this.
func Register(api huma.API, svc *Service) {
	useTypedErrors()

	huma.Register(api, huma.Operation{
		OperationID:   "request-verification",
		Method:        http.MethodPost,
		Path:          "/auth/verifications",
		Summary:       "Request a WhatsApp verification code",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusCreated,
		Errors: []int{
			http.StatusConflict,            // account_exists
			http.StatusNotFound,            // no_account (signin)
			http.StatusUnprocessableEntity, // invalid_phone
			http.StatusTooManyRequests,     // cooldown_active
			http.StatusLocked,              // send-cap reached
			http.StatusBadGateway,          // send_failed
			http.StatusNotImplemented,      // unsupported intent
		},
	}, func(ctx context.Context, in *requestVerificationInput) (*requestVerificationOutput, error) {
		v, err := svc.RequestVerification(ctx, RequestVerificationParams{
			Intent:          Intent(in.Body.Intent),
			Phone:           in.Body.Phone,
			Role:            Role(in.Body.Role),
			Language:        Language(in.Body.Language),
			GoogleLinkToken: in.Body.GoogleLinkToken,
		})
		if err != nil {
			return nil, toHTTPError(ctx, err)
		}
		out := &requestVerificationOutput{}
		out.Body.VerificationID = v.ID.String()
		out.Body.ExpiresAt = v.ExpiresAt
		out.Body.ResendAvailableAt = v.ResendAvailableAt()
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "resend-verification",
		Method:      http.MethodPost,
		Path:        "/auth/verifications/{id}/resend",
		Summary:     "Resend the verification code",
		Tags:        []string{"auth"},
		Errors: []int{
			http.StatusNotFound,        // not_found
			http.StatusGone,            // expired
			http.StatusLocked,          // send-cap reached
			http.StatusTooManyRequests, // cooldown_active
			http.StatusBadGateway,      // send_failed
		},
	}, func(ctx context.Context, in *resendVerificationInput) (*resendVerificationOutput, error) {
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, notFoundError()
		}
		v, err := svc.ResendVerification(ctx, id)
		if err != nil {
			return nil, toHTTPError(ctx, err)
		}
		out := &resendVerificationOutput{}
		out.Body.ResendAvailableAt = v.ResendAvailableAt()
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "confirm-verification",
		Method:      http.MethodPost,
		Path:        "/auth/verifications/{id}/confirm",
		Summary:     "Confirm the verification code",
		Tags:        []string{"auth"},
		Errors: []int{
			http.StatusNotFound,            // not_found
			http.StatusGone,                // expired
			http.StatusUnprocessableEntity, // incorrect_code
			http.StatusLocked,              // locked
		},
	}, func(ctx context.Context, in *confirmVerificationInput) (*confirmVerificationOutput, error) {
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, notFoundError()
		}
		res, err := svc.ConfirmVerification(ctx, id, in.Body.Code)
		if err != nil {
			return nil, toHTTPError(ctx, err)
		}
		out := &confirmVerificationOutput{}
		out.Body.SessionToken = res.SessionToken
		out.Body.Account.ID = res.Account.ID.String()
		out.Body.Account.Role = string(res.Account.Role)
		out.Body.Account.Language = string(res.Account.PreferredLanguage)
		out.Body.Route = res.Route
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "google-exchange",
		Method:      http.MethodPost,
		Path:        "/auth/google",
		Summary:     "Exchange a Google ID token for a session or a signup route",
		Tags:        []string{"auth"},
		Errors: []int{
			http.StatusUnauthorized, // google_failed
		},
	}, func(ctx context.Context, in *googleExchangeInput) (*googleExchangeOutput, error) {
		var role *Role
		if in.Body.Role != "" {
			r := Role(in.Body.Role)
			role = &r
		}
		res, err := svc.GoogleExchange(ctx, in.Body.IDToken, role)
		if err != nil {
			return nil, toHTTPError(ctx, err)
		}
		out := &googleExchangeOutput{}
		if res.NoAccount {
			out.Body.Next = "verify_phone"
			out.Body.PhonePrefill = res.PhonePrefill
			out.Body.PendingLinkToken = res.PendingLinkToken
			return out, nil
		}
		out.Body.SessionToken = res.SessionToken
		out.Body.Account = &struct {
			ID       string `json:"id"`
			Role     string `json:"role"`
			Language string `json:"language"`
		}{
			ID:       res.Account.ID.String(),
			Role:     string(res.Account.Role),
			Language: string(res.Account.PreferredLanguage),
		}
		out.Body.Route = res.Route
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-session",
		Method:      http.MethodGet,
		Path:        "/auth/session",
		Summary:     "Validate the current session",
		Tags:        []string{"auth"},
		Middlewares: platformauth.SessionMiddleware(svc),
		Errors:      []int{http.StatusUnauthorized},
	}, func(ctx context.Context, in *sessionInput) (*getSessionOutput, error) {
		principal, _ := platformauth.PrincipalFromContext(ctx)
		accountID, err := uuid.Parse(principal.AccountID)
		if err != nil {
			return nil, huma.Error401Unauthorized("invalid session")
		}
		account, err := svc.Account(ctx, accountID)
		if err != nil {
			return nil, toHTTPError(ctx, err)
		}
		out := &getSessionOutput{}
		out.Body.Account.ID = account.ID.String()
		out.Body.Account.Role = string(account.Role)
		out.Body.Account.Language = string(account.PreferredLanguage)
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "sign-out",
		Method:        http.MethodDelete,
		Path:          "/auth/session",
		Summary:       "Sign out the current device",
		Tags:          []string{"auth"},
		Middlewares:   platformauth.SessionMiddleware(svc),
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusUnauthorized},
	}, func(ctx context.Context, in *sessionInput) (*struct{}, error) {
		token := strings.TrimPrefix(in.Authorization, "Bearer ")
		if err := svc.RevokeSession(ctx, token); err != nil {
			return nil, toHTTPError(ctx, err)
		}
		return nil, nil
	})

	// Admin operations sit behind the coarse is-admin gate: one configured shared
	// secret, carrying one configured display identity for the audit trail.
	// svc.adminAPIKey/adminID default to "" until WithAdmin is called, which
	// AdminMiddleware treats as fail closed.
	adminMW := platformauth.AdminMiddleware(svc.adminAPIKey, svc.adminID)

	huma.Register(api, huma.Operation{
		OperationID: "admin-list-accounts",
		Method:      http.MethodGet,
		Path:        "/admin/accounts",
		Summary:     "List/search registered accounts",
		Tags:        []string{"admin"},
		Middlewares: adminMW,
		Errors:      []int{http.StatusUnauthorized},
	}, func(ctx context.Context, in *listAccountsInput) (*listAccountsOutput, error) {
		items, err := svc.ListAccounts(ctx, in.Phone, in.Limit, in.Offset)
		if err != nil {
			return nil, toHTTPError(ctx, err)
		}
		out := &listAccountsOutput{Body: make([]accountListItemDTO, len(items))}
		for i, item := range items {
			out.Body[i] = accountListItemDTO{
				ID:            item.ID.String(),
				Role:          string(item.Role),
				Phone:         item.Phone,
				GoogleLinked:  item.GoogleLinked,
				SignupMethod:  item.SignupMethod,
				CreatedAt:     item.CreatedAt,
				LockoutActive: item.LockoutActive,
			}
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "admin-clear-lockout",
		Method:      http.MethodPost,
		Path:        "/admin/accounts/{id}/clear-lockout",
		Summary:     "Clear an active verification lockout on an account's phone",
		Tags:        []string{"admin"},
		Middlewares: adminMW,
		Errors: []int{
			http.StatusUnauthorized,
			http.StatusNotFound,
		},
	}, func(ctx context.Context, in *clearLockoutInput) (*clearLockoutOutput, error) {
		accountID, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, notFoundError()
		}
		// actorAdminID always comes from the server-verified AdminPrincipal
		// AdminMiddleware attached to ctx — never from the request path or body,
		// which a client fully controls.
		admin, _ := platformauth.AdminPrincipalFromContext(ctx)
		cleared, err := svc.ClearLockout(ctx, admin.ID, accountID)
		if err != nil {
			return nil, toHTTPError(ctx, err)
		}
		out := &clearLockoutOutput{}
		out.Body.Cleared = cleared
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "admin-analytics",
		Method:      http.MethodGet,
		Path:        "/admin/analytics",
		Summary:     "Aggregate signup/authentication analytics over a time range",
		Tags:        []string{"admin"},
		Middlewares: adminMW,
		Errors: []int{
			http.StatusUnauthorized,
			http.StatusUnprocessableEntity, // invalid_range
		},
	}, func(ctx context.Context, in *analyticsInput) (*analyticsOutput, error) {
		res, err := svc.Analytics(ctx, in.Range)
		if err != nil {
			return nil, toHTTPError(ctx, err)
		}
		out := &analyticsOutput{}
		out.Body.SignupsByMethod = res.SignupsByMethod
		out.Body.VerificationFailureRate = res.VerificationFailureRate
		out.Body.LockoutCount = res.LockoutCount
		return out, nil
	})
}

// apiError is the feature's typed error body. Huma writes a returned
// StatusError value directly as the JSON response, giving clients a stable
// `code` string plus the contract's extra fields. Its exported fields and the
// Code enum are what Huma reflects into the generated OpenAPI error responses,
// so admin/mobile clients see the typed shape.
type apiError struct {
	status            int
	Code              string     `json:"code" enum:"invalid_phone,account_exists,no_account,incorrect_code,locked,expired,cooldown_active,send_failed,not_found,not_implemented,bad_request,unprocessable_entity,unauthorized,forbidden,google_failed,internal_error,invalid_range" doc:"Stable machine-readable error code"`
	Message           string     `json:"message" doc:"Human-readable error message"`
	LockedUntil       *time.Time `json:"locked_until,omitempty" doc:"When a lockout lifts (RFC3339); present on locked errors"`
	AttemptsRemaining *int       `json:"attempts_remaining,omitempty" doc:"Wrong-code attempts left before lockout; present on incorrect_code"`
}

func (e *apiError) Error() string  { return e.Message }
func (e *apiError) GetStatus() int { return e.status }

// useTypedErrors routes every Huma-constructed error through apiError, so
// framework validation and content-negotiation failures carry the same typed
// `code` as the service errors toHTTPError maps by hand. huma.NewError is a
// package-level hook, so this is a process-wide assignment: it runs from
// Register rather than init() to keep it an act of wiring rather than an import
// side effect, and once because Register may be called more than once.
var useTypedErrors = sync.OnceFunc(func() {
	huma.NewError = func(status int, msg string, _ ...error) huma.StatusError {
		return &apiError{status: status, Code: codeForStatus(status), Message: msg}
	}
})

// codeForStatus gives framework-originated errors a stable code drawn from the
// same set as the service's own mapped codes.
func codeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusUnprocessableEntity:
		return "unprocessable_entity"
	case http.StatusNotImplemented:
		return "not_implemented"
	default:
		return "internal_error"
	}
}

// notFoundError is the generic 404 body for any resource lookup by id
// (verification, and now account) that comes up empty — including a malformed
// id in the path, which fails uuid.Parse before any lookup runs.
func notFoundError() *apiError {
	return &apiError{status: http.StatusNotFound, Code: "not_found", Message: "not found"}
}

// toHTTPError maps a service sentinel error to a typed API error. Every
// service return path must land on a case here; anything unmapped is logged
// and returned as a generic 500 so no wrapped internal detail leaks to the
// client.
func toHTTPError(ctx context.Context, err error) error {
	var incErr *IncorrectCodeError
	var lockErr *LockedError

	switch {
	case errors.Is(err, ErrInvalidPhone):
		return &apiError{status: http.StatusUnprocessableEntity, Code: "invalid_phone", Message: "not a valid Egyptian mobile number"}
	case errors.Is(err, ErrAccountExists):
		return &apiError{status: http.StatusConflict, Code: "account_exists", Message: "an account already exists for this phone number"}
	case errors.Is(err, ErrNoAccount):
		return &apiError{status: http.StatusNotFound, Code: "no_account", Message: "no account exists for this phone number"}
	case errors.As(err, &incErr):
		remaining := incErr.AttemptsRemaining
		return &apiError{status: http.StatusUnprocessableEntity, Code: "incorrect_code", Message: "the verification code is incorrect", AttemptsRemaining: &remaining}
	case errors.Is(err, ErrIncorrectCode):
		return &apiError{status: http.StatusUnprocessableEntity, Code: "incorrect_code", Message: "the verification code is incorrect"}
	case errors.As(err, &lockErr):
		lockedUntil := lockErr.LockedUntil
		return &apiError{status: http.StatusLocked, Code: "locked", Message: "too many attempts; the challenge is temporarily locked", LockedUntil: &lockedUntil}
	case errors.Is(err, ErrExpired):
		return &apiError{status: http.StatusGone, Code: "expired", Message: "the verification code has expired"}
	case errors.Is(err, ErrResendCooldown):
		return &apiError{status: http.StatusTooManyRequests, Code: "cooldown_active", Message: "please wait before requesting another code"}
	case errors.Is(err, ErrSendFailed):
		return &apiError{status: http.StatusBadGateway, Code: "send_failed", Message: "could not send the verification code; please retry"}
	case errors.Is(err, ErrNotFound):
		return notFoundError()
	case errors.Is(err, ErrUnsupportedIntent):
		return &apiError{status: http.StatusNotImplemented, Code: "not_implemented", Message: "this authentication flow is not yet supported"}
	case errors.Is(err, ErrGoogleFailed):
		return &apiError{status: http.StatusUnauthorized, Code: "google_failed", Message: "could not verify the Google account"}
	case errors.Is(err, ErrNoSession):
		return &apiError{status: http.StatusUnauthorized, Code: "unauthorized", Message: "no valid session"}
	case errors.Is(err, ErrInvalidRange):
		return &apiError{status: http.StatusUnprocessableEntity, Code: "invalid_range", Message: "range must be one of 24h, 7d, 30d"}
	default:
		observability.LoggerFromContext(ctx).Error("auth: unhandled service error", "error", err)
		return huma.Error500InternalServerError("internal error")
	}
}
