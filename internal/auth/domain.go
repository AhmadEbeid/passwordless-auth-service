// Package auth is the passwordless phone-signup feature slice: WhatsApp-OTP
// verification, account creation on confirm, and opaque per-device sessions.
// This file is the pure core — entities, enums, and business rules with no
// I/O. All timed rules take the current time as an argument so the service can
// drive them from an injected Clock; domain code never reads the wall clock
// itself.
package auth

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Role is the immutable account kind chosen at signup.
type Role string

const (
	RoleCoach   Role = "coach"
	RoleAthlete Role = "athlete"
)

// Valid reports whether r is a known role.
func (r Role) Valid() bool { return r == RoleCoach || r == RoleAthlete }

// Language is the consumer app's preferred language.
type Language string

const (
	LanguageArabic  Language = "ar"
	LanguageEnglish Language = "en"
)

// Valid reports whether l is a known language.
func (l Language) Valid() bool { return l == LanguageArabic || l == LanguageEnglish }

// Intent distinguishes the sign-up flow from the sign-in flow: signup runs the
// duplicate-phone check, signin runs the no-account check.
type Intent string

const (
	IntentSignup Intent = "signup"
	IntentSignin Intent = "signin"
)

// Valid reports whether i is a known intent.
func (i Intent) Valid() bool { return i == IntentSignup || i == IntentSignin }

// Status is the lifecycle state of a Verification challenge.
type Status string

const (
	StatusPending  Status = "pending"
	StatusVerified Status = "verified"
	StatusExpired  Status = "expired"
	StatusLocked   Status = "locked"
)

// Timed rule constants: the spec-fixed OTP/lockout/cooldown values, plus the
// chosen 60-day session idle-expiry and 5-sends-per-30-min send cap.
const (
	OTPTTL            = 10 * time.Minute
	LockoutDuration   = 30 * time.Minute
	ResendCooldown    = 60 * time.Second
	MaxFailedAttempts = 5
	MaxSendsPerWindow = 5
	SendCapWindow     = 30 * time.Minute
	SessionIdleWindow = 60 * 24 * time.Hour
	OTPDigits         = 6
	// SessionExtendInterval throttles the rolling-expiry write; see
	// Session.NeedsExtension.
	SessionExtendInterval = 24 * time.Hour
)

// Sentinel errors. Callers match with errors.Is; the handler maps each to a
// typed API error code.
var (
	ErrAccountExists     = errors.New("auth: account already exists for phone")
	ErrNoAccount         = errors.New("auth: no account for phone")
	ErrLocked            = errors.New("auth: verification locked")
	ErrIncorrectCode     = errors.New("auth: incorrect code")
	ErrExpired           = errors.New("auth: verification expired")
	ErrSendFailed        = errors.New("auth: verification send failed")
	ErrInvalidPhone      = errors.New("auth: invalid phone number")
	ErrResendCooldown    = errors.New("auth: resend cooldown active")
	ErrNotFound          = errors.New("auth: not found")
	ErrUnsupportedIntent = errors.New("auth: unsupported intent")
	ErrGoogleFailed      = errors.New("auth: google verification failed")
	ErrNoSession         = errors.New("auth: no valid session")
	ErrInvalidRange      = errors.New("auth: invalid analytics range")
)

// IncorrectCodeError wraps ErrIncorrectCode with the attempts a caller has
// left before the challenge locks. errors.Is(err, ErrIncorrectCode) still
// holds.
type IncorrectCodeError struct {
	AttemptsRemaining int
}

func (e *IncorrectCodeError) Error() string {
	return fmt.Sprintf("auth: incorrect code: %d attempts remaining", e.AttemptsRemaining)
}

func (e *IncorrectCodeError) Unwrap() error { return ErrIncorrectCode }

// LockedError wraps ErrLocked with the time the lockout lifts. errors.Is(err,
// ErrLocked) still holds.
type LockedError struct {
	LockedUntil time.Time
}

func (e *LockedError) Error() string {
	return fmt.Sprintf("auth: verification locked until %s", e.LockedUntil.Format(time.RFC3339))
}

func (e *LockedError) Unwrap() error { return ErrLocked }

// Account is a verified user. The row exists only after a Verification is
// confirmed; there is no password field anywhere.
type Account struct {
	ID                uuid.UUID
	Role              Role
	Phone             string
	GoogleSubject     *string
	GoogleEmail       *string
	PreferredLanguage Language
	CreatedAt         time.Time
}

// Verification is the transient OTP challenge. It carries the pending-signup
// context so confirm can construct the account, which does not exist yet.
type Verification struct {
	ID                   uuid.UUID
	Phone                string
	Intent               Intent
	CodeHash             string
	PendingRole          Role
	PendingLanguage      Language
	PendingGoogleSubject *string
	PendingGoogleEmail   *string
	SentAt               time.Time
	ExpiresAt            time.Time
	FailedAttempts       int
	SendsInWindow        int
	LockedUntil          *time.Time
	Status               Status
}

// Session is an opaque, per-device, revocable session. Only the token hash is
// stored.
type Session struct {
	ID           uuid.UUID
	AccountID    uuid.UUID
	TokenHash    string
	DeviceLabel  *string
	CreatedAt    time.Time
	LastActiveAt time.Time
	ExpiresAt    time.Time
	RevokedAt    *time.Time
}

// IsValid reports whether the session is usable at now: not revoked and not
// past its rolling idle-expiry.
func (s *Session) IsValid(now time.Time) bool {
	return s.RevokedAt == nil && now.Before(s.ExpiresAt)
}

// Extend rolls the idle-expiry window forward from now. Callers must persist.
func (s *Session) Extend(now time.Time) {
	s.LastActiveAt = now
	s.ExpiresAt = now.Add(SessionIdleWindow)
}

// NeedsExtension reports whether the idle window has drifted far enough to be
// worth a write. Without this every authenticated request would issue an
// UPDATE to push an expiry that is already 60 days out.
func (s *Session) NeedsExtension(now time.Time) bool {
	return !now.Before(s.LastActiveAt.Add(SessionExtendInterval))
}

// GoogleIdentity is the verified result of a Google ID token.
type GoogleIdentity struct {
	Subject       string
	Email         string
	EmailVerified bool
}

// AuditEvent is a minimal, append-only record of an administrator action that
// changed an account's authentication state. It is never updated or deleted.
type AuditEvent struct {
	ID              uuid.UUID
	ActorAdminID    string
	Action          string
	TargetAccountID uuid.UUID
	CreatedAt       time.Time
	Metadata        map[string]any
}

// Audit action names. Exactly one emitter for now.
const AuditActionClearLockout = "clear_lockout"

// SignupMethod values for the admin account list, derived from whether
// GoogleSubject is set at account creation rather than stored separately:
// account linking/merging after creation is out of scope (spec Assumptions),
// so GoogleSubject fully and permanently determines which path created the
// account.
const (
	SignupMethodPhone  = "phone"
	SignupMethodGoogle = "google"
)

// Client routes returned on a successful confirm/Google-resume: onboarding for
// a brand-new account, home for a returning one.
const (
	RouteOnboarding = "onboarding"
	RouteHome       = "home"
)

// IsExpired reports whether the challenge is past its expiry at now.
func (v *Verification) IsExpired(now time.Time) bool {
	return now.After(v.ExpiresAt)
}

// IsLocked reports whether the challenge is locked at now. The lock is
// time-boxed by LockedUntil (30 min) and lifts once it passes; the locked
// status is a denormalization, not the authority.
func (v *Verification) IsLocked(now time.Time) bool {
	return v.LockedUntil != nil && now.Before(*v.LockedUntil)
}

// ResendReady reports whether the resend cooldown has elapsed at now.
func (v *Verification) ResendReady(now time.Time) bool {
	return !now.Before(v.SentAt.Add(ResendCooldown))
}

// ResendAvailableAt is the earliest time a resend is permitted.
func (v *Verification) ResendAvailableAt() time.Time {
	return v.SentAt.Add(ResendCooldown)
}

// SendCapReached reports whether the send-cap for the window is exhausted, so
// the next send must lock instead.
func (v *Verification) SendCapReached() bool {
	return v.SendsInWindow >= MaxSendsPerWindow
}

// RemainingAttempts is how many wrong-code attempts remain before lockout.
func (v *Verification) RemainingAttempts() int {
	r := MaxFailedAttempts - v.FailedAttempts
	if r < 0 {
		return 0
	}
	return r
}

// RegisterSend records a delivered code: it advances the send counter and
// restarts the 10-min expiry window from now. Callers must persist.
func (v *Verification) RegisterSend(now time.Time) {
	v.SendsInWindow++
	v.SentAt = now
	v.ExpiresAt = now.Add(OTPTTL)
}

// RegisterFailure records a wrong-code attempt and locks the challenge on the
// fifth. It returns true when the attempt caused a lockout. Callers must
// persist.
func (v *Verification) RegisterFailure(now time.Time) (locked bool) {
	v.FailedAttempts++
	if v.FailedAttempts >= MaxFailedAttempts {
		v.Lock(now)
		return true
	}
	return false
}

// Lock puts the challenge into the locked state for LockoutDuration from now.
// Callers must persist.
func (v *Verification) Lock(now time.Time) {
	until := now.Add(LockoutDuration)
	v.LockedUntil = &until
	v.Status = StatusLocked
}

// MarkVerified transitions a confirmed challenge to verified. Callers persist.
func (v *Verification) MarkVerified() { v.Status = StatusVerified }

// NormalizePhone converts a raw Egyptian mobile number to canonical E.164 (+20
// followed by exactly 10 significant digits). It accepts a leading 0 trunk
// prefix or a pasted +20, and rejects anything else.
func NormalizePhone(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "-", "")

	switch {
	case strings.HasPrefix(s, "+20"):
		s = s[len("+20"):]
	case strings.HasPrefix(s, "0"):
		s = s[len("0"):]
	}

	if len(s) != 10 || !isAllDigits(s) {
		return "", ErrInvalidPhone
	}
	return "+20" + s, nil
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}
