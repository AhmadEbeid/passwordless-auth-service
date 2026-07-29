// Package auth is the platform seam for authentication + coarse (role)
// authorization: it verifies a request's credential and carries the resulting
// principal in context. It never touches persistence directly — the consumer
// session mechanism is a port implemented by the auth feature; the admin
// mechanism is self-contained and needs no such port.
package auth

import "context"

// Principal is an authenticated consumer session (a signed-in account).
type Principal struct {
	AccountID string
	Role      string
}

// AdminPrincipal is an authenticated admin request.
type AdminPrincipal struct {
	ID string
}

type principalKey struct{}

type adminPrincipalKey struct{}

// ContextWithPrincipal attaches a consumer principal to ctx.
func ContextWithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFromContext returns the consumer principal attached by
// SessionMiddleware, if any.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// ContextWithAdminPrincipal attaches an admin principal to ctx.
func ContextWithAdminPrincipal(ctx context.Context, p AdminPrincipal) context.Context {
	return context.WithValue(ctx, adminPrincipalKey{}, p)
}

// AdminPrincipalFromContext returns the admin principal attached by
// AdminMiddleware, if any.
func AdminPrincipalFromContext(ctx context.Context) (AdminPrincipal, bool) {
	p, ok := ctx.Value(adminPrincipalKey{}).(AdminPrincipal)
	return p, ok
}
