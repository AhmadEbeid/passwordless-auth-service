package observability_test

import (
	"context"
	"testing"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/observability"
)

func TestLoggerFromContextFallsBack(t *testing.T) {
	if observability.LoggerFromContext(context.Background()) == nil {
		t.Fatal("expected a non-nil fallback logger")
	}
}

func TestNewLoggerLevels(t *testing.T) {
	if observability.NewLogger("debug") == nil {
		t.Fatal("expected a logger")
	}
	if observability.NewLogger("bogus") == nil {
		t.Fatal("expected a logger even for an unknown level")
	}
}
