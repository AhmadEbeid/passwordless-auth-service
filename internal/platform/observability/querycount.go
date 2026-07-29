package observability

import (
	"context"
	"sync/atomic"
)

// Per-request statement counting. An N+1 is invisible by latency alone on a
// development dataset — one query per row and one query for all rows look the
// same at ten rows — so the count is what separates them. The db pool's tracer
// calls CountQuery for every statement, the HTTP middleware installs a tally
// per request and logs the total, and tests assert an upper bound to keep a
// batched access pattern from silently regressing into a per-row one.

// QueryCountThreshold is the per-request statement count above which the
// request is logged at warn level. Chosen to sit above the handful of
// statements a normal request makes and below anything that looks per-row.
const QueryCountThreshold = 20

// ContextWithQueryCount returns a ctx carrying a fresh statement tally.
func ContextWithQueryCount(ctx context.Context) context.Context {
	return context.WithValue(ctx, queryCountKey, new(atomic.Int64))
}

// CountQuery increments the tally on ctx. It is a no-op when none is installed,
// so background jobs and tests that do not opt in pay nothing.
func CountQuery(ctx context.Context) {
	if c, ok := ctx.Value(queryCountKey).(*atomic.Int64); ok {
		c.Add(1)
	}
}

// QueryCount reports the statements run under ctx, or 0 when no tally was
// installed.
func QueryCount(ctx context.Context) int {
	if c, ok := ctx.Value(queryCountKey).(*atomic.Int64); ok {
		return int(c.Load())
	}
	return 0
}
