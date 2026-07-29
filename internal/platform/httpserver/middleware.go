// Package httpserver wires chi + Huma, middleware, health checks, and graceful
// shutdown. It never imports pgx (depguard); it depends on the Pinger
// interface.
package httpserver

import (
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/observability"
)

const requestIDHeader = "X-Request-ID"

// withRequestContext assigns/propagates a request id and a request-scoped
// logger, making both available via context for the rest of the chain. It also
// installs the statement tally the db tracer feeds, so a request that queries
// per row instead of in a batch is visible in the logs rather than only in a
// latency graph.
func withRequestContext(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(requestIDHeader)
			if id == "" {
				id = uuid.NewString()
			}
			w.Header().Set(requestIDHeader, id)
			l := base.With("request_id", id)
			ctx := observability.ContextWithRequestID(r.Context(), id)
			ctx = observability.ContextWithLogger(ctx, l)
			ctx = observability.ContextWithQueryCount(ctx)

			next.ServeHTTP(w, r.WithContext(ctx))

			if n := observability.QueryCount(ctx); n >= observability.QueryCountThreshold {
				l.WarnContext(ctx, "request issued an unusual number of database statements",
					"method", r.Method, "path", r.URL.Path, "queries", n)
			}
		})
	}
}
