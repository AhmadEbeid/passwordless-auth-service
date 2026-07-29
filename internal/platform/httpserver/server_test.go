package httpserver_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/config"
	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/httpserver"
)

func TestServer_RunStopsOnContextCancel(t *testing.T) {
	r, _ := httpserver.NewRouter(slog.Default(), fakePinger{}, nil)
	srv := httpserver.New(config.Config{HTTPPort: "0"}, r) // :0 = random free port

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error on shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down within 5s")
	}
}
