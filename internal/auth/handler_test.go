package auth_test

import (
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/google/uuid"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/auth"
)

// Admin gate test fixture: a fixed key/id, configured via Service.WithAdmin,
// standing in for the deployment's real ADMIN_API_KEY/ ADMIN_ID.
const (
	testAdminAPIKey = "test-only-admin-key"
	testAdminID     = "test-ops"
)

// TestGetSessionRequiresValidSession proves GET /auth/session rejects a
// missing or unrecognized bearer token with 401, exercising the real
// platform/auth SessionMiddleware wiring end-to-end.
func TestGetSessionRequiresValidSession(t *testing.T) {
	h := newHarness()
	_, api := humatest.New(t)
	auth.Register(api, h.svc)

	if resp := api.Get("/auth/session"); resp.Code != 401 {
		t.Fatalf("no-token status = %d, want 401; body=%s", resp.Code, resp.Body.String())
	}
	if resp := api.Get("/auth/session", "Authorization: Bearer not-a-real-token"); resp.Code != 401 {
		t.Fatalf("unknown-token status = %d, want 401; body=%s", resp.Code, resp.Body.String())
	}
}

// TestGetSessionReturnsAccountForValidSession proves a valid bearer token
// returns 200 with the account it belongs to.
func TestGetSessionReturnsAccountForValidSession(t *testing.T) {
	h := newHarness()
	_, api := humatest.New(t)
	auth.Register(api, h.svc)

	token, accountID := signUpAndConfirm(t, h)

	resp := api.Get("/auth/session", "Authorization: Bearer "+token)
	if resp.Code != 200 {
		t.Fatalf("status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), accountID.String()) {
		t.Fatalf("body missing account id %q: %s", accountID, resp.Body.String())
	}
}

// TestSignOutRevokesSessionThenGetSessionFails proves DELETE /auth/session
// actually revokes the session at the HTTP layer, not just in the service unit
// tests.
func TestSignOutRevokesSessionThenGetSessionFails(t *testing.T) {
	h := newHarness()
	_, api := humatest.New(t)
	auth.Register(api, h.svc)

	token, _ := signUpAndConfirm(t, h)

	if resp := api.Delete("/auth/session", "Authorization: Bearer "+token); resp.Code != 204 {
		t.Fatalf("sign-out status = %d, want 204; body=%s", resp.Code, resp.Body.String())
	}
	if resp := api.Get("/auth/session", "Authorization: Bearer "+token); resp.Code != 401 {
		t.Fatalf("status after sign-out = %d, want 401; body=%s", resp.Code, resp.Body.String())
	}
}

// --- admin endpoints, behind the coarse admin gate ---

// TestAdminListAccountsRequiresAdminKey proves the gate rejects no/wrong
// Authorization headers and accepts the configured key.
func TestAdminListAccountsRequiresAdminKey(t *testing.T) {
	h := newHarness()
	h.svc.WithAdmin(testAdminAPIKey, testAdminID)
	_, api := humatest.New(t)
	auth.Register(api, h.svc)

	if resp := api.Get("/admin/accounts"); resp.Code != 401 {
		t.Fatalf("no key status = %d, want 401; body=%s", resp.Code, resp.Body.String())
	}
	if resp := api.Get("/admin/accounts", "Authorization: Bearer wrong-key"); resp.Code != 401 {
		t.Fatalf("wrong key status = %d, want 401; body=%s", resp.Code, resp.Body.String())
	}
	if resp := api.Get("/admin/accounts", "Authorization: Bearer "+testAdminAPIKey); resp.Code != 200 {
		t.Fatalf("correct key status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
}

// TestAdminListAccountsReturnsSeededAccounts proves the happy path renders the
// contract's fields and the phone filter narrows results.
func TestAdminListAccountsReturnsSeededAccounts(t *testing.T) {
	h := newHarness()
	h.svc.WithAdmin(testAdminAPIKey, testAdminID)
	_, api := humatest.New(t)
	auth.Register(api, h.svc)

	h.accounts.byPhone["+201066666666"] = &auth.Account{ID: uuid.New(), Phone: "+201066666666", Role: auth.RoleCoach, CreatedAt: base}

	resp := api.Get("/admin/accounts?phone=6666", "Authorization: Bearer "+testAdminAPIKey)
	if resp.Code != 200 {
		t.Fatalf("status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	for _, want := range []string{"+201066666666", `"role":"coach"`, `"signup_method":"phone"`, `"google_linked":false`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}

	if resp := api.Get("/admin/accounts?phone=no-match", "Authorization: Bearer "+testAdminAPIKey); !strings.Contains(resp.Body.String(), "[]") {
		t.Fatalf("non-matching filter should return an empty list: %s", resp.Body.String())
	}
}

// TestAdminClearLockoutRequiresAdminKey proves the gate rejects no/wrong
// Authorization headers.
func TestAdminClearLockoutRequiresAdminKey(t *testing.T) {
	h := newHarness()
	h.svc.WithAdmin(testAdminAPIKey, testAdminID)
	_, api := humatest.New(t)
	auth.Register(api, h.svc)

	path := "/admin/accounts/" + uuid.New().String() + "/clear-lockout"
	if resp := api.Post(path); resp.Code != 401 {
		t.Fatalf("no key status = %d, want 401; body=%s", resp.Code, resp.Body.String())
	}
	if resp := api.Post(path, "Authorization: Bearer wrong-key"); resp.Code != 401 {
		t.Fatalf("wrong key status = %d, want 401; body=%s", resp.Code, resp.Body.String())
	}
}

// TestAdminClearLockoutHappyPath drives an account into lockout, clears it via
// the HTTP endpoint, and proves the response and the audited actor come from
// the server-verified admin principal, never the request.
func TestAdminClearLockoutHappyPath(t *testing.T) {
	h := newHarness()
	h.svc.WithAdmin(testAdminAPIKey, testAdminID)
	_, api := humatest.New(t)
	auth.Register(api, h.svc)

	const rawPhone3 = "01077777777"
	const normalizedPhone3 = "+201077777777"
	accountID := uuid.New()
	h.accounts.byPhone[normalizedPhone3] = &auth.Account{ID: accountID, Phone: normalizedPhone3, Role: auth.RoleAthlete, CreatedAt: base}

	v, err := h.svc.RequestVerification(t.Context(), auth.RequestVerificationParams{Intent: auth.IntentSignin, Phone: rawPhone3})
	if err != nil {
		t.Fatalf("RequestVerification: %v", err)
	}
	wrong := wrongCodeFor(h.sender.lastCode)
	for attempt := 1; attempt <= auth.MaxFailedAttempts; attempt++ {
		_, _ = h.svc.ConfirmVerification(t.Context(), v.ID, wrong)
	}

	resp := api.Post("/admin/accounts/"+accountID.String()+"/clear-lockout", "Authorization: Bearer "+testAdminAPIKey)
	if resp.Code != 200 {
		t.Fatalf("status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"cleared":true`) {
		t.Fatalf("body = %s, want cleared:true", resp.Body.String())
	}
	if len(h.audit.created) != 1 || h.audit.created[0].ActorAdminID != testAdminID {
		t.Fatalf("audit events = %+v, want exactly one with actor %q", h.audit.created, testAdminID)
	}
}

// TestAdminAnalyticsRequiresAdminKey proves the gate rejects no/wrong
// Authorization headers and accepts the configured key.
func TestAdminAnalyticsRequiresAdminKey(t *testing.T) {
	h := newHarness()
	h.svc.WithAdmin(testAdminAPIKey, testAdminID)
	_, api := humatest.New(t)
	auth.Register(api, h.svc)

	if resp := api.Get("/admin/analytics?range=7d"); resp.Code != 401 {
		t.Fatalf("no key status = %d, want 401; body=%s", resp.Code, resp.Body.String())
	}
	if resp := api.Get("/admin/analytics?range=7d", "Authorization: Bearer wrong-key"); resp.Code != 401 {
		t.Fatalf("wrong key status = %d, want 401; body=%s", resp.Code, resp.Body.String())
	}
	if resp := api.Get("/admin/analytics?range=7d", "Authorization: Bearer "+testAdminAPIKey); resp.Code != 200 {
		t.Fatalf("correct key status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
}
