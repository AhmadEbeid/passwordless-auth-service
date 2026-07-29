package httpserver

import (
	"context"
	"net/http"
)

// Pinger reports backing-store readiness. *pgxpool.Pool satisfies it.
type Pinger interface {
	Ping(ctx context.Context) error
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func readyz(ready Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := ready.Ping(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"unavailable"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	}
}
