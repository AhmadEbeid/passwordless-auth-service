package auth_test

import (
	"context"
	"testing"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/auth"
)

func TestPrincipalRoundTrip(t *testing.T) {
	want := auth.Principal{AccountID: "acct-1", Role: "coach"}
	ctx := auth.ContextWithPrincipal(context.Background(), want)

	got, ok := auth.PrincipalFromContext(ctx)
	if !ok {
		t.Fatal("PrincipalFromContext reported no principal")
	}
	if got != want {
		t.Fatalf("principal = %+v, want %+v", got, want)
	}
}

func TestAdminPrincipalRoundTrip(t *testing.T) {
	want := auth.AdminPrincipal{ID: "ops-1"}
	ctx := auth.ContextWithAdminPrincipal(context.Background(), want)

	got, ok := auth.AdminPrincipalFromContext(ctx)
	if !ok {
		t.Fatal("AdminPrincipalFromContext reported no principal")
	}
	if got != want {
		t.Fatalf("admin principal = %+v, want %+v", got, want)
	}
}

func TestPrincipalFromBareContextIsAbsent(t *testing.T) {
	ctx := context.Background()

	if p, ok := auth.PrincipalFromContext(ctx); ok {
		t.Errorf("PrincipalFromContext on a bare context returned %+v, want absent", p)
	}
	if p, ok := auth.AdminPrincipalFromContext(ctx); ok {
		t.Errorf("AdminPrincipalFromContext on a bare context returned %+v, want absent", p)
	}
}

// The two principals must not be reachable through each other's accessor.
// Handlers decide what a caller may do from which lookup succeeds, so a
// signed-in member whose context satisfied AdminPrincipalFromContext would be
// an admin, and the separation has to be structural rather than incidental.
func TestPrincipalKindsDoNotCollide(t *testing.T) {
	member := auth.ContextWithPrincipal(context.Background(),
		auth.Principal{AccountID: "acct-1", Role: "coach"})
	if p, ok := auth.AdminPrincipalFromContext(member); ok {
		t.Errorf("a consumer principal satisfied AdminPrincipalFromContext: %+v", p)
	}

	admin := auth.ContextWithAdminPrincipal(context.Background(),
		auth.AdminPrincipal{ID: "ops-1"})
	if p, ok := auth.PrincipalFromContext(admin); ok {
		t.Errorf("an admin principal satisfied PrincipalFromContext: %+v", p)
	}
}

// Both may coexist without overwriting each other.
func TestBothPrincipalsCanCoexist(t *testing.T) {
	ctx := auth.ContextWithPrincipal(context.Background(),
		auth.Principal{AccountID: "acct-1", Role: "athlete"})
	ctx = auth.ContextWithAdminPrincipal(ctx, auth.AdminPrincipal{ID: "ops-1"})

	p, ok := auth.PrincipalFromContext(ctx)
	if !ok || p.AccountID != "acct-1" {
		t.Errorf("consumer principal = %+v, %v; want acct-1", p, ok)
	}
	a, ok := auth.AdminPrincipalFromContext(ctx)
	if !ok || a.ID != "ops-1" {
		t.Errorf("admin principal = %+v, %v; want ops-1", a, ok)
	}
}
