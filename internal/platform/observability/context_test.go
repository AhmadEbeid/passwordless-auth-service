package observability_test

import (
	"context"
	"testing"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/observability"
)

func TestRequestIDRoundTrip(t *testing.T) {
	ctx := observability.ContextWithRequestID(context.Background(), "req-123")
	if got := observability.RequestIDFromContext(ctx); got != "req-123" {
		t.Fatalf("RequestIDFromContext = %q, want req-123", got)
	}
}

func TestRequestIDMissing(t *testing.T) {
	if got := observability.RequestIDFromContext(context.Background()); got != "" {
		t.Fatalf("expected empty request id, got %q", got)
	}
}
