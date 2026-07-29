package observability

import (
	"context"
	"log/slog"
)

type ctxKey int

const (
	loggerKey ctxKey = iota
	requestIDKey
	queryCountKey
)

// ContextWithLogger stores a request-scoped logger in ctx.
func ContextWithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

// LoggerFromContext returns the request-scoped logger, or slog.Default() if
// none.
func LoggerFromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}

// ContextWithRequestID stores the request id in ctx.
func ContextWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestIDFromContext returns the request id, or "" if none.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}
