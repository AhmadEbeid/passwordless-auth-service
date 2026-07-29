package httpserver_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/httpserver"
)

func TestRequestID_GeneratedWhenAbsent(t *testing.T) {
	r, _ := httpserver.NewRouter(slog.Default(), fakePinger{}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)

	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-ID"); got == "" {
		t.Fatal("X-Request-ID header not set on response")
	}
}

func TestRequestID_PropagatedFromRequest(t *testing.T) {
	r, _ := httpserver.NewRouter(slog.Default(), fakePinger{}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", "fixed-id-123")

	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-ID"); got != "fixed-id-123" {
		t.Fatalf("X-Request-ID = %q, want %q", got, "fixed-id-123")
	}
}
