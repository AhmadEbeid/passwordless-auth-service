// Package observability wires structured logging, request-scoped context, and
// an OpenTelemetry tracer seam. No log line bypasses slog.
package observability

import (
	"log/slog"
	"os"
	"strings"
)

// NewLogger returns a JSON slog logger at the given level
// ("debug"|"info"|"warn"|"error"). Unknown levels default to info.
func NewLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(h)
}
