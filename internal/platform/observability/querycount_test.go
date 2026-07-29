package observability_test

import (
	"context"
	"sync"
	"testing"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/observability"
)

func TestQueryCountWithoutCounterIsNoop(t *testing.T) {
	ctx := context.Background()
	observability.CountQuery(ctx) // must not panic on a bare context
	if n := observability.QueryCount(ctx); n != 0 {
		t.Fatalf("QueryCount on a bare context = %d, want 0", n)
	}
}

func TestQueryCountTallies(t *testing.T) {
	ctx := observability.ContextWithQueryCount(context.Background())
	for range 3 {
		observability.CountQuery(ctx)
	}
	if n := observability.QueryCount(ctx); n != 3 {
		t.Fatalf("QueryCount = %d, want 3", n)
	}
}

// A pool hands connections to concurrent goroutines, so the tally has to be
// safe to increment from several at once.
func TestQueryCountIsConcurrencySafe(t *testing.T) {
	ctx := observability.ContextWithQueryCount(context.Background())

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			observability.CountQuery(ctx)
		}()
	}
	wg.Wait()

	if n := observability.QueryCount(ctx); n != 50 {
		t.Fatalf("QueryCount = %d, want 50", n)
	}
}

// Counters are per-context, so one request's statements never land on another's
// tally.
func TestQueryCountIsScopedPerContext(t *testing.T) {
	base := context.Background()
	a := observability.ContextWithQueryCount(base)
	b := observability.ContextWithQueryCount(base)

	observability.CountQuery(a)
	observability.CountQuery(a)
	observability.CountQuery(b)

	if n := observability.QueryCount(a); n != 2 {
		t.Fatalf("QueryCount(a) = %d, want 2", n)
	}
	if n := observability.QueryCount(b); n != 1 {
		t.Fatalf("QueryCount(b) = %d, want 1", n)
	}
}
