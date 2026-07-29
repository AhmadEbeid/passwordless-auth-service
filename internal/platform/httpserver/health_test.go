package httpserver_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/httpserver"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func TestHealthz_AlwaysOK(t *testing.T) {
	r, _ := httpserver.NewRouter(slog.Default(), fakePinger{}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz = %d, want 200", rec.Code)
	}
}

func TestReadyz_OKWhenDBUp(t *testing.T) {
	r, _ := httpserver.NewRouter(slog.Default(), fakePinger{}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("readyz = %d, want 200", rec.Code)
	}
}

func TestReadyz_503WhenDBDown(t *testing.T) {
	r, _ := httpserver.NewRouter(slog.Default(), fakePinger{err: errors.New("down")}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz = %d, want 503", rec.Code)
	}
}
