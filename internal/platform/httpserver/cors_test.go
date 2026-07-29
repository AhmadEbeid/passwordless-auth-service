package httpserver_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/httpserver"
)

func TestCORS_NoOriginsConfigured_NoHeaders(t *testing.T) {
	r, _ := httpserver.NewRouter(slog.Default(), fakePinger{}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "http://localhost:5183")

	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty when no origins configured", got)
	}
}

func TestCORS_AllowedOrigin_HeaderPresent(t *testing.T) {
	r, _ := httpserver.NewRouter(slog.Default(), fakePinger{}, []string{"http://localhost:5183"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "http://localhost:5183")

	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5183" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, "http://localhost:5183")
	}
}

func TestCORS_DisallowedOrigin_HeaderAbsent(t *testing.T) {
	r, _ := httpserver.NewRouter(slog.Default(), fakePinger{}, []string{"http://localhost:5183"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "http://evil.example")

	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty for a disallowed origin", got)
	}
}

func TestCORS_Preflight_AllowsAuthorizationHeader(t *testing.T) {
	r, _ := httpserver.NewRouter(slog.Default(), fakePinger{}, []string{"http://localhost:5183"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodOptions, "/admin/accounts", nil)
	req.Header.Set("Origin", "http://localhost:5183")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	req.Header.Set("Access-Control-Request-Headers", "Authorization")

	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5183" {
		t.Fatalf("preflight Access-Control-Allow-Origin = %q, want %q", got, "http://localhost:5183")
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Fatal("preflight Access-Control-Allow-Headers not set")
	}
}
