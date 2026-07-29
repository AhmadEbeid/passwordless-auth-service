package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/config"
)

// NewRouter builds the chi router (health + middleware) and mounts a Huma API
// on it for feature handlers to register operations against.
// adminAllowedOrigins enables CORS for the admin dashboard (a browser app,
// unlike mobile's native client) — empty by default, so no CORS headers are
// sent until configured.
func NewRouter(logger *slog.Logger, ready Pinger, adminAllowedOrigins []string) (*chi.Mux, huma.API) {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(withRequestContext(logger))
	if len(adminAllowedOrigins) > 0 {
		r.Use(cors.Handler(cors.Options{
			AllowedOrigins: adminAllowedOrigins,
			AllowedMethods: []string{"GET", "POST", "DELETE"},
			AllowedHeaders: []string{"Authorization", "Content-Type"},
			MaxAge:         300,
		}))
	}

	r.Get("/healthz", healthz)
	r.Get("/readyz", readyz(ready))

	api := humachi.New(r, huma.DefaultConfig("Passwordless Auth API", "1.0.0"))
	return r, api
}

// Server owns the http.Server lifecycle with graceful shutdown.
type Server struct {
	httpServer *http.Server
}

func New(cfg config.Config, handler http.Handler) *Server {
	return &Server{
		httpServer: &http.Server{
			Addr:              net.JoinHostPort("", cfg.HTTPPort),
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
		},
	}
}

// Run serves until ctx is cancelled, then drains connections (10s timeout).
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		// A fresh context.Background() is intentional here, not an oversight: ctx is
		// already Done at this point, so deriving the shutdown timeout from it would
		// produce an already-expired context.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return s.httpServer.Shutdown(shutdownCtx) //nolint:contextcheck // shutdown must outlive the cancelled ctx that triggered it
	}
}
