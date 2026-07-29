package observability

import (
	"context"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/config"
)

// NewTracerProvider returns a TracerProvider registered globally. It has NO
// exporter yet: spans are produced but dropped until one is wired. Callers
// must Shutdown it on exit.
func NewTracerProvider(ctx context.Context, cfg config.Config) (*sdktrace.TracerProvider, error) {
	_ = ctx
	_ = cfg
	// AlwaysSample now; add an exporter + batcher when tracing is turned on.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	return tp, nil
}
