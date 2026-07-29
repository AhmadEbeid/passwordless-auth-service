package auth

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Clock is the single source of time for every timed rule. Injecting it makes
// expiry, lockout, cooldown, and send-cap deterministically testable.
// Production wires a clock backed by time.Now.
type Clock interface {
	Now() time.Time
}

// VerificationSender delivers a plaintext OTP to a phone over the provider
// channel (WhatsApp in production). It is a consumer-owned port: the real Meta
// WhatsApp Cloud API adapter sits behind it and is deferrable to ops; tests
// and the composition-root stub implement it directly.
type VerificationSender interface {
	SendCode(ctx context.Context, phone, code string) error
}

// AccountRepository persists and looks up verified accounts. Implementations
// return ErrNotFound when no account matches and map storage-level uniqueness
// violations to ErrAccountExists.
type AccountRepository interface {
	FindByPhone(ctx context.Context, phone string) (*Account, error)
	FindByID(ctx context.Context, id uuid.UUID) (*Account, error)
	FindByGoogleSubject(ctx context.Context, subject string) (*Account, error)
	Create(ctx context.Context, account *Account) error
	// List returns accounts whose phone contains phoneFilter, newest-created
	// first. An empty filter matches every account.
	List(ctx context.Context, phoneFilter string, limit, offset int) ([]Account, error)
	// CountCreatedSince splits accounts created at or after since into phone-only
	// vs Google-linked counts, for admin analytics' signups_by_method.
	CountCreatedSince(ctx context.Context, since time.Time) (phoneOnly, googleLinked int, err error)
}

// VerificationRepository persists OTP challenges. Get returns ErrNotFound when
// no challenge matches the id.
type VerificationRepository interface {
	Create(ctx context.Context, v *Verification) error
	Get(ctx context.Context, id uuid.UUID) (*Verification, error)
	Update(ctx context.Context, v *Verification) error
	// CountSendsSince sums the sends recorded for phone whose last send lands at
	// or after since, approximating the rolling send-cap window.
	CountSendsSince(ctx context.Context, phone string, since time.Time) (int, error)
	// FindLatestByPhone returns the most recently sent challenge for phone, or
	// ErrNotFound if the phone has none.
	FindLatestByPhone(ctx context.Context, phone string) (*Verification, error)
	// LockedPhones reports which of phones have their most recently sent
	// challenge locked at now. Batched so the admin list does not issue one
	// lookup per row.
	LockedPhones(ctx context.Context, phones []string, now time.Time) (map[string]bool, error)
	// SumFailedAttemptsSince sums failed_attempts across challenges sent at or
	// after since, for the admin failure-rate metric.
	SumFailedAttemptsSince(ctx context.Context, since time.Time) (int, error)
	// CountByStatusSince counts challenges with the given status sent at or after
	// since, for admin analytics: the verified count (the successful-confirm side
	// of verification_failure_rate) and the locked count (lockout_count).
	CountByStatusSince(ctx context.Context, status Status, since time.Time) (int, error)
}

// SessionRepository persists issued sessions.
type SessionRepository interface {
	Create(ctx context.Context, s *Session) error
	// FindByTokenHash returns ErrNotFound when no session matches.
	FindByTokenHash(ctx context.Context, tokenHash string) (*Session, error)
	// Update persists a session's rolling-idle-expiry extension.
	Update(ctx context.Context, s *Session) error
	// RevokeByTokenHash sets revoked_at on the matching session. Revoking an
	// already-revoked or missing session is a no-op, not an error — sign-out is
	// idempotent.
	RevokeByTokenHash(ctx context.Context, tokenHash string, revokedAt time.Time) error
}

// IdentityVerifier verifies a Google ID token and returns the identity it
// proves. It is a consumer-owned port; the real adapter (an Meta Cloud API
// analogue for Google) is deferrable to ops.
type IdentityVerifier interface {
	Verify(ctx context.Context, idToken string) (*GoogleIdentity, error)
}

// AuditEventRepository persists admin audit events. Append-only: no update or
// delete method exists.
type AuditEventRepository interface {
	Create(ctx context.Context, e *AuditEvent) error
}

// TxRunner runs a set of writes inside a single database transaction. It is a
// consumer-owned port so the service stays free of any platform/db or pgx
// import; the platform TxManager satisfies it. Repos invoked inside fn pick up
// the ambient transaction from the passed ctx.
type TxRunner interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}
