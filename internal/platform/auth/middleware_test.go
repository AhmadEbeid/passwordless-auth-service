package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/auth"
)

const (
	testAdminKey = "correct-horse-battery-staple"
	testAdminID  = "ops-1"
)

// fakeValidator accepts exactly one token, standing in for the auth feature's
// Service. Anything else fails, which is how the middleware sees a revoked,
// expired or forged token.
type fakeValidator struct {
	token string
	want  auth.Principal
	calls int
}

func (v *fakeValidator) ValidateSession(_ context.Context, token string) (auth.Principal, error) {
	v.calls++
	if token != v.token {
		return auth.Principal{}, errors.New("no valid session")
	}
	return v.want, nil
}

// probe records what the handler behind the middleware saw, so a test can
// distinguish "rejected" from "ran but saw nothing".
type probe struct {
	reached  bool
	user     auth.Principal
	sawUser  bool
	admin    auth.AdminPrincipal
	sawAdmin bool
}

type probeOutput struct{ Body struct{} }

// mount registers a probe operation guarded by mw and returns the test API plus
// the probe. Using huma.Register rather than calling the middleware func
// directly means the test exercises the same wiring production uses.
func mount(t *testing.T, mw huma.Middlewares) (humatest.TestAPI, *probe) {
	t.Helper()
	_, api := humatest.New(t)
	p := &probe{}

	huma.Register(api, huma.Operation{
		OperationID: "probe",
		Method:      http.MethodGet,
		Path:        "/probe",
		Middlewares: mw,
		Errors:      []int{http.StatusUnauthorized},
	}, func(ctx context.Context, _ *struct{}) (*probeOutput, error) {
		p.reached = true
		p.user, p.sawUser = auth.PrincipalFromContext(ctx)
		p.admin, p.sawAdmin = auth.AdminPrincipalFromContext(ctx)
		return &probeOutput{}, nil
	})
	return api, p
}

// --- SessionMiddleware ---

func TestSessionMiddlewareRejectsMalformedCredentials(t *testing.T) {
	cases := []struct {
		name    string
		headers []any
	}{
		{"no header", nil},
		{"empty header", []any{"Authorization: "}},
		{"bearer with no token", []any{"Authorization: Bearer "}},
		{"not a bearer scheme", []any{"Authorization: Basic dXNlcjpwdw=="}},
		{"raw token without scheme", []any{"Authorization: some-token"}},
		{"lowercase scheme", []any{"Authorization: bearer some-token"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := &fakeValidator{token: "good"}
			api, p := mount(t, auth.SessionMiddleware(v))

			resp := api.Get("/probe", tc.headers...)
			if resp.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body=%s", resp.Code, resp.Body.String())
			}
			if p.reached {
				t.Error("handler ran despite a rejected credential")
			}
			// A malformed credential must be rejected on shape alone, without
			// spending a session lookup on it.
			if v.calls != 0 {
				t.Errorf("validator called %d times, want 0", v.calls)
			}
		})
	}
}

func TestSessionMiddlewareRejectsInvalidToken(t *testing.T) {
	v := &fakeValidator{token: "good"}
	api, p := mount(t, auth.SessionMiddleware(v))

	resp := api.Get("/probe", "Authorization: Bearer wrong")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", resp.Code, resp.Body.String())
	}
	if p.reached {
		t.Error("handler ran despite an invalid token")
	}
	if v.calls != 1 {
		t.Errorf("validator called %d times, want 1", v.calls)
	}
}

func TestSessionMiddlewareAttachesPrincipal(t *testing.T) {
	want := auth.Principal{AccountID: "acct-1", Role: "coach"}
	v := &fakeValidator{token: "good", want: want}
	api, p := mount(t, auth.SessionMiddleware(v))

	resp := api.Get("/probe", "Authorization: Bearer good")
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	if !p.reached {
		t.Fatal("handler did not run for a valid session")
	}
	if !p.sawUser || p.user != want {
		t.Fatalf("handler saw principal %+v (%v), want %+v", p.user, p.sawUser, want)
	}
}

// A consumer session must not confer admin rights. If this middleware ever
// attached an AdminPrincipal, every signed-in member would pass the admin gate.
func TestSessionMiddlewareGrantsNoAdminPrincipal(t *testing.T) {
	v := &fakeValidator{token: "good", want: auth.Principal{AccountID: "acct-1", Role: "coach"}}
	api, p := mount(t, auth.SessionMiddleware(v))

	if resp := api.Get("/probe", "Authorization: Bearer good"); resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	if p.sawAdmin {
		t.Errorf("a consumer session produced an admin principal: %+v", p.admin)
	}
}

// The token travels in a header, so it must not come back in the error body.
func TestSessionMiddlewareRejectionIsTypedJSONWithoutTheToken(t *testing.T) {
	const token = "super-secret-session-token"
	v := &fakeValidator{token: "good"}
	api, _ := mount(t, auth.SessionMiddleware(v))

	resp := api.Get("/probe", "Authorization: Bearer "+token)
	if got := resp.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	body := resp.Body.String()
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("rejection body is not JSON (%v): %s", err, body)
	}
	if len(parsed) == 0 {
		t.Errorf("rejection body is an empty object: %s", body)
	}
	if strings.Contains(body, token) {
		t.Errorf("rejection body echoes the submitted token: %s", body)
	}
}

// --- AdminMiddleware ---

// The gate is the only thing standing in front of /admin/*, so an unconfigured
// deployment must reject everything rather than accept anything.
func TestAdminMiddlewareFailsClosedWhenUnconfigured(t *testing.T) {
	cases := []struct {
		name    string
		headers []any
	}{
		{"no credential at all", nil},
		{"an empty bearer token", []any{"Authorization: Bearer "}},
		{"a plausible-looking key", []any{"Authorization: Bearer " + testAdminKey}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api, p := mount(t, auth.AdminMiddleware("", testAdminID))

			resp := api.Get("/probe", tc.headers...)
			if resp.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body=%s", resp.Code, resp.Body.String())
			}
			if p.reached {
				t.Error("handler ran with the admin gate unconfigured")
			}
		})
	}
}

func TestAdminMiddlewareRejectsWrongCredentials(t *testing.T) {
	cases := []struct {
		name    string
		headers []any
	}{
		{"no header", nil},
		{"bearer with no token", []any{"Authorization: Bearer "}},
		{"wrong key", []any{"Authorization: Bearer nope"}},
		{"not a bearer scheme", []any{"Authorization: " + testAdminKey}},
		// A compare that stopped at the shorter operand would accept these.
		{"prefix of the key", []any{"Authorization: Bearer " + testAdminKey[:10]}},
		{"key plus a suffix", []any{"Authorization: Bearer " + testAdminKey + "x"}},
		{"key with different case", []any{"Authorization: Bearer " + "CORRECT-horse-battery-staple"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api, p := mount(t, auth.AdminMiddleware(testAdminKey, testAdminID))

			resp := api.Get("/probe", tc.headers...)
			if resp.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body=%s", resp.Code, resp.Body.String())
			}
			if p.reached {
				t.Error("handler ran despite a rejected admin credential")
			}
		})
	}
}

func TestAdminMiddlewareAttachesConfiguredIdentity(t *testing.T) {
	api, p := mount(t, auth.AdminMiddleware(testAdminKey, testAdminID))

	resp := api.Get("/probe", "Authorization: Bearer "+testAdminKey)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	if !p.reached {
		t.Fatal("handler did not run for a valid admin key")
	}
	// Audit events name this id, and it comes from configuration rather than
	// from anything the caller sent.
	if !p.sawAdmin || p.admin.ID != testAdminID {
		t.Fatalf("handler saw admin %+v (%v), want id %q", p.admin, p.sawAdmin, testAdminID)
	}
}

// Passing the admin gate must not manufacture a consumer session, or an admin
// request would be attributed to an account it never authenticated as.
func TestAdminMiddlewareGrantsNoConsumerPrincipal(t *testing.T) {
	api, p := mount(t, auth.AdminMiddleware(testAdminKey, testAdminID))

	if resp := api.Get("/probe", "Authorization: Bearer "+testAdminKey); resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	if p.sawUser {
		t.Errorf("the admin gate produced a consumer principal: %+v", p.user)
	}
}

func TestAdminMiddlewareRejectionIsTypedJSONWithoutTheKey(t *testing.T) {
	api, _ := mount(t, auth.AdminMiddleware(testAdminKey, testAdminID))

	resp := api.Get("/probe", "Authorization: Bearer wrong-key")
	if got := resp.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	body := resp.Body.String()
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("rejection body is not JSON (%v): %s", err, body)
	}
	if strings.Contains(body, testAdminKey) {
		t.Errorf("rejection body leaks the configured admin key: %s", body)
	}
}
