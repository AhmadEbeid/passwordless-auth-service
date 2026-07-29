package whatsapp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/observability"
	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/whatsapp"
)

// capture returns a ctx carrying a logger at level writing JSON into buf.
func capture(level slog.Level) (context.Context, *bytes.Buffer) {
	var buf bytes.Buffer
	l := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: level}))
	return observability.ContextWithLogger(context.Background(), l), &buf
}

func TestLoggingSenderRecordsTheCode(t *testing.T) {
	ctx, buf := capture(slog.LevelDebug)

	if err := whatsapp.NewLoggingSender().SendCode(ctx, "+201012345678", "123456"); err != nil {
		t.Fatalf("SendCode: %v", err)
	}

	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("log line is not JSON (%v): %s", err, buf.String())
	}
	if entry["code"] != "123456" {
		t.Errorf("logged code = %v, want 123456", entry["code"])
	}
	if entry["phone"] != "+201012345678" {
		t.Errorf("logged phone = %v, want +201012345678", entry["phone"])
	}
}

// The code is a live credential. Logging it at debug is what keeps it out of an
// aggregated log sink when a deployment runs at the default info level, so this
// sender must stay silent there rather than merely being chosen less often.
func TestLoggingSenderStaysSilentAtInfo(t *testing.T) {
	ctx, buf := capture(slog.LevelInfo)

	if err := whatsapp.NewLoggingSender().SendCode(ctx, "+201012345678", "123456"); err != nil {
		t.Fatalf("SendCode: %v", err)
	}

	if strings.Contains(buf.String(), "123456") {
		t.Errorf("code reached an info-level log: %s", buf.String())
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output at info level, got: %s", buf.String())
	}
}

// It must never fail: a deployment without credentials still has to complete
// signups, and a returned error would surface as send_failed.
func TestLoggingSenderNeverFails(t *testing.T) {
	sender := whatsapp.NewLoggingSender()

	// Including on a context with no logger attached at all.
	if err := sender.SendCode(context.Background(), "+201012345678", "123456"); err != nil {
		t.Fatalf("SendCode with no logger in context: %v", err)
	}
	if err := sender.SendCode(context.Background(), "", ""); err != nil {
		t.Fatalf("SendCode with empty arguments: %v", err)
	}
}
