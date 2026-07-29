package whatsapp

import (
	"context"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/observability"
)

// LoggingSender delivers nothing and writes the code to the request logger
// instead. It is the sender a deployment without Meta credentials runs on, so
// the signup flow stays walkable locally and in tests that exercise the real
// HTTP path.
//
// It logs at debug precisely because the code is a live credential: at the
// default info level the OTP never reaches a log sink, so choosing this sender
// cannot quietly start leaking codes into aggregated production logs.
type LoggingSender struct{}

// NewLoggingSender returns the no-delivery sender. The composition root picks
// it when no Meta credentials are configured, and logs that it did.
func NewLoggingSender() LoggingSender { return LoggingSender{} }

func (LoggingSender) SendCode(ctx context.Context, phone, code string) error {
	observability.LoggerFromContext(ctx).Debug("whatsapp: code not sent, no credentials configured",
		"phone", phone, "code", code)
	return nil
}
