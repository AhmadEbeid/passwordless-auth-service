package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	platformauth "github.com/AhmadEbeid/passwordless-auth-service/internal/platform/auth"
)

// Service orchestrates the passwordless signup use cases against its ports. It
// is pgx-free: all persistence goes through the repository ports and all time
// through the Clock.
type Service struct {
	clock         Clock
	sender        VerificationSender
	identity      IdentityVerifier
	accounts      AccountRepository
	verifications VerificationRepository
	sessions      SessionRepository
	auditEvents   AuditEventRepository
	tx            TxRunner
	// linkSecret signs the short-lived pending-Google-link token (google.go); see
	// NewService's doc for why this must never be derived from client input.
	linkSecret []byte
	// Both default to "" until WithAdmin is called, which AdminMiddleware treats
	// as fail closed — the same as a misconfigured deployment.
	adminAPIKey string
	adminID     string
}

// NewService is the only construction path. linkSecret must be a stable,
// server-only secret: it is what lets RequestVerification trust a pending
// Google identity without re-verifying the ID token, so a guessable one would
// let an attacker link an account to a Google identity they do not own.
func NewService(
	clock Clock,
	sender VerificationSender,
	identity IdentityVerifier,
	accounts AccountRepository,
	verifications VerificationRepository,
	sessions SessionRepository,
	auditEvents AuditEventRepository,
	tx TxRunner,
	linkSecret []byte,
) *Service {
	return &Service{
		clock:         clock,
		sender:        sender,
		identity:      identity,
		accounts:      accounts,
		verifications: verifications,
		sessions:      sessions,
		auditEvents:   auditEvents,
		tx:            tx,
		linkSecret:    linkSecret,
	}
}

// WithAdmin sets the admin gate's shared secret and the identity recorded on
// admin-originated audit events, and returns s for chaining. Call once from
// the composition root; leaving it uncalled keeps the gate closed.
func (s *Service) WithAdmin(apiKey, adminID string) *Service {
	s.adminAPIKey = apiKey
	s.adminID = adminID
	return s
}

// ConfirmResult is what a successful confirm yields: the plaintext session
// token (returned once, never stored), the new account, and the client route.
type ConfirmResult struct {
	SessionToken string
	Account      Account
	Route        string
}

// RequestVerificationParams configures a verification challenge request.
// GoogleLinkToken threads an already-verified Google identity into a signup
// challenge, so confirm can link the account on creation without a second
// OAuth round trip. It must be the token GoogleExchange minted for that exact
// identity — never a client-supplied subject or email, which would let anyone
// link an account to a Google identity they do not own. Signup only.
type RequestVerificationParams struct {
	Intent          Intent
	Phone           string
	Role            Role
	Language        Language
	GoogleLinkToken *string
}

// RequestVerification starts a signup or signin challenge. Signup rejects a
// duplicate account and signin rejects an unregistered phone, both before any
// code is sent. It then generates, sends, and persists a fresh OTP. A provider
// send failure returns ErrSendFailed and persists nothing, so a retry consumes
// no attempt and leaves no orphaned challenge.
func (s *Service) RequestVerification(ctx context.Context, params RequestVerificationParams) (*Verification, error) {
	switch params.Intent {
	case IntentSignup, IntentSignin:
	default:
		return nil, ErrUnsupportedIntent
	}

	normalized, err := NormalizePhone(params.Phone)
	if err != nil {
		return nil, err
	}

	// Both checks run before any code is sent.
	switch _, err := s.accounts.FindByPhone(ctx, normalized); {
	case err == nil:
		if params.Intent == IntentSignup {
			return nil, ErrAccountExists
		}
	case errors.Is(err, ErrNotFound):
		if params.Intent == IntentSignin {
			return nil, ErrNoAccount
		}
	default:
		return nil, fmt.Errorf("auth: request verification: find account: %w", err)
	}

	now := s.clock.Now()

	// Before anything with a side effect: a forged or expired token must not
	// consume a real WhatsApp send.
	var pendingGoogleSubject, pendingGoogleEmail *string
	if params.Intent == IntentSignup && params.GoogleLinkToken != nil {
		subject, email, err := verifyPendingGoogleLink(s.linkSecret, *params.GoogleLinkToken, now)
		if err != nil {
			return nil, err
		}
		pendingGoogleSubject, pendingGoogleEmail = &subject, &email
	}

	// Checked here, not only on resend: otherwise a locked-out phone could bypass
	// its lockout by starting a fresh request. The rolling send-cap window is
	// approximated by summing sends_in_window over the phone's rows inside the
	// window; an exact per-send ledger is deferred.
	var latest *Verification
	switch v, err := s.verifications.FindLatestByPhone(ctx, normalized); {
	case err == nil:
		latest = v
		if latest.IsLocked(now) {
			return nil, &LockedError{LockedUntil: lockedUntil(latest, now)}
		}
		if !latest.ResendReady(now) {
			return nil, ErrResendCooldown
		}
	case errors.Is(err, ErrNotFound):
		// No prior challenge for this phone — proceed.
	default:
		return nil, fmt.Errorf("auth: request verification: find latest: %w", err)
	}

	sends, err := s.verifications.CountSendsSince(ctx, normalized, now.Add(-SendCapWindow))
	if err != nil {
		return nil, fmt.Errorf("auth: request verification: count sends: %w", err)
	}
	if sends >= MaxSendsPerWindow {
		// Persist the lock on the phone's latest row so it stays observable and
		// clearable by an admin, exactly like one reached via resend or repeated
		// failed codes. Which endpoint tripped it must not matter.
		if latest != nil {
			latest.Lock(now)
			if err := s.verifications.Update(ctx, latest); err != nil {
				return nil, fmt.Errorf("auth: request verification: lock: %w", err)
			}
			return nil, &LockedError{LockedUntil: lockedUntil(latest, now)}
		}
		return nil, &LockedError{LockedUntil: now.Add(LockoutDuration)}
	}

	code, err := generateOTP()
	if err != nil {
		return nil, err
	}
	codeHash, err := hashCode(code)
	if err != nil {
		return nil, err
	}

	// Send before persisting: a failed send leaves no challenge behind.
	if err := s.sender.SendCode(ctx, normalized, code); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSendFailed, err)
	}

	v := &Verification{
		ID:       uuid.New(),
		Phone:    normalized,
		Intent:   params.Intent,
		CodeHash: codeHash,
		Status:   StatusPending,
	}
	if params.Intent == IntentSignup {
		v.PendingRole = params.Role
		v.PendingLanguage = params.Language
		v.PendingGoogleSubject = pendingGoogleSubject
		v.PendingGoogleEmail = pendingGoogleEmail
	}
	v.RegisterSend(now)

	if err := s.verifications.Create(ctx, v); err != nil {
		return nil, fmt.Errorf("auth: request verification: create: %w", err)
	}
	return v, nil
}

// ConfirmVerification checks the submitted code. A wrong code increments the
// attempt counter and the fifth locks the challenge; an expired or locked
// challenge is rejected. On success it creates the account for a signup
// challenge or resumes the existing one for signin, issues a session either
// way, and returns the plaintext token plus the client route.
func (s *Service) ConfirmVerification(ctx context.Context, id uuid.UUID, code string) (*ConfirmResult, error) {
	v, err := s.verifications.Get(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("auth: confirm verification: get: %w", err)
	}

	now := s.clock.Now()

	if v.IsLocked(now) {
		return nil, &LockedError{LockedUntil: lockedUntil(v, now)}
	}
	if v.Status == StatusVerified {
		// Challenge already consumed; the code is no longer valid.
		return nil, ErrIncorrectCode
	}
	if v.IsExpired(now) {
		return nil, ErrExpired
	}

	if err := bcrypt.CompareHashAndPassword([]byte(v.CodeHash), []byte(code)); err != nil {
		locked := v.RegisterFailure(now)
		if uerr := s.verifications.Update(ctx, v); uerr != nil {
			return nil, fmt.Errorf("auth: confirm verification: update: %w", uerr)
		}
		if locked {
			return nil, &LockedError{LockedUntil: lockedUntil(v, now)}
		}
		return nil, &IncorrectCodeError{AttemptsRemaining: v.RemainingAttempts()}
	}

	// Correct code — only now resolve the account: create it for signup, or look
	// up the existing one for signin.
	token, tokenHash, err := newSessionToken()
	if err != nil {
		return nil, err
	}
	session := &Session{
		ID:           uuid.New(),
		TokenHash:    tokenHash,
		CreatedAt:    now,
		LastActiveAt: now,
		ExpiresAt:    now.Add(SessionIdleWindow),
	}

	route := RouteOnboarding
	if v.Intent == IntentSignin {
		route = RouteHome
	}

	// One transaction: a failure between these steps must not leave an account
	// with no session, or a challenge still pending, which would strand the user.
	// Repos pick up the ambient tx from the closure's ctx.
	var account *Account
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if v.Intent == IntentSignin {
			existing, ferr := s.accounts.FindByPhone(ctx, v.Phone)
			if ferr != nil {
				if errors.Is(ferr, ErrNotFound) {
					// RequestVerification already checked the account exists; this would mean
					// it vanished between request and confirm.
					return fmt.Errorf("auth: confirm verification: signin account missing: %w", ErrNoAccount)
				}
				return fmt.Errorf("auth: confirm verification: find account: %w", ferr)
			}
			account = existing
		} else {
			account = &Account{
				ID:                uuid.New(),
				Role:              v.PendingRole,
				Phone:             v.Phone,
				PreferredLanguage: v.PendingLanguage,
				CreatedAt:         now,
			}
			if v.PendingGoogleSubject != nil {
				account.GoogleSubject = v.PendingGoogleSubject
				account.GoogleEmail = v.PendingGoogleEmail
			}
			if err := s.accounts.Create(ctx, account); err != nil {
				if errors.Is(err, ErrAccountExists) {
					return ErrAccountExists
				}
				return fmt.Errorf("auth: confirm verification: create account: %w", err)
			}
		}

		session.AccountID = account.ID
		if err := s.sessions.Create(ctx, session); err != nil {
			return fmt.Errorf("auth: confirm verification: create session: %w", err)
		}
		v.MarkVerified()
		if err := s.verifications.Update(ctx, v); err != nil {
			return fmt.Errorf("auth: confirm verification: update: %w", err)
		}
		return nil
	}); err != nil {
		if errors.Is(err, ErrAccountExists) {
			return nil, ErrAccountExists
		}
		if errors.Is(err, ErrNoAccount) {
			return nil, ErrNoAccount
		}
		return nil, err
	}

	return &ConfirmResult{
		SessionToken: token,
		Account:      *account,
		Route:        route,
	}, nil
}

// GoogleExchangeResult is what a Google ID-token exchange yields: either an
// existing account, populated exactly like a signin confirm with no OTP
// involved, or no account at all, in which case the client collects a phone
// and calls RequestVerification to finish the signup.
type GoogleExchangeResult struct {
	SessionToken string
	Account      Account
	Route        string

	NoAccount bool
	// Always nil today: a Google ID token carries no phone claim, so there is
	// nothing to prefill from Google alone.
	PhonePrefill *string
	// Set only when NoAccount: a server-signed, short-lived proof of the verified
	// identity for the client to hand back to RequestVerification, so the account
	// is linked to the identity actually just proven rather than a raw subject
	// the client could swap for someone else's.
	PendingLinkToken string
}

// GoogleExchange verifies a Google ID token and either resumes the session of
// the account already linked to it, or reports that none is, so the client can
// start the phone step. role is accepted for contract symmetry but unused:
// there is nothing to persist until confirm creates the account.
func (s *Service) GoogleExchange(ctx context.Context, idToken string, role *Role) (*GoogleExchangeResult, error) {
	identity, err := s.identity.Verify(ctx, idToken)
	if err != nil {
		return nil, fmt.Errorf("auth: google exchange: verify: %w", err)
	}

	account, err := s.accounts.FindByGoogleSubject(ctx, identity.Subject)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			token, err := signPendingGoogleLink(s.linkSecret, identity, s.clock.Now())
			if err != nil {
				return nil, err
			}
			return &GoogleExchangeResult{NoAccount: true, PendingLinkToken: token}, nil
		}
		return nil, fmt.Errorf("auth: google exchange: find account: %w", err)
	}

	now := s.clock.Now()
	token, tokenHash, err := newSessionToken()
	if err != nil {
		return nil, err
	}
	session := &Session{
		ID:           uuid.New(),
		AccountID:    account.ID,
		TokenHash:    tokenHash,
		CreatedAt:    now,
		LastActiveAt: now,
		ExpiresAt:    now.Add(SessionIdleWindow),
	}
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.sessions.Create(ctx, session); err != nil {
			return fmt.Errorf("auth: google exchange: create session: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return &GoogleExchangeResult{
		SessionToken: token,
		Account:      *account,
		Route:        RouteHome,
	}, nil
}

// ResendVerification re-sends a fresh code for an existing challenge, subject
// to the cooldown and the send-cap; exceeding the cap locks the challenge. A
// provider send failure returns ErrSendFailed and mutates no state, so the
// cooldown does not restart and the send is not counted.
func (s *Service) ResendVerification(ctx context.Context, id uuid.UUID) (*Verification, error) {
	v, err := s.verifications.Get(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("auth: resend verification: get: %w", err)
	}

	now := s.clock.Now()

	if v.IsLocked(now) {
		return nil, &LockedError{LockedUntil: lockedUntil(v, now)}
	}
	if v.Status == StatusVerified || v.IsExpired(now) {
		return nil, ErrExpired
	}
	if !v.ResendReady(now) {
		return nil, ErrResendCooldown
	}
	if v.SendCapReached() {
		v.Lock(now)
		if err := s.verifications.Update(ctx, v); err != nil {
			return nil, fmt.Errorf("auth: resend verification: update: %w", err)
		}
		return nil, &LockedError{LockedUntil: lockedUntil(v, now)}
	}

	code, err := generateOTP()
	if err != nil {
		return nil, err
	}
	codeHash, err := hashCode(code)
	if err != nil {
		return nil, err
	}

	// Send before mutating state: a failed send is not counted.
	if err := s.sender.SendCode(ctx, v.Phone, code); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSendFailed, err)
	}

	v.CodeHash = codeHash
	v.RegisterSend(now)
	if err := s.verifications.Update(ctx, v); err != nil {
		return nil, fmt.Errorf("auth: resend verification: update: %w", err)
	}
	return v, nil
}

// lockedUntil returns the challenge's lockout expiry, falling back to a fresh
// window from now if the row somehow carries no timestamp.
func lockedUntil(v *Verification, now time.Time) time.Time {
	if v.LockedUntil != nil {
		return *v.LockedUntil
	}
	return now.Add(LockoutDuration)
}

func generateOTP() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("auth: generate otp: %w", err)
	}
	return fmt.Sprintf("%0*d", OTPDigits, n.Int64()), nil
}

// hashCode hashes a low-entropy OTP with bcrypt: a DB read must not reveal a
// live code, and bcrypt's work factor is what makes the 6-digit space
// impractical to brute-force offline.
func hashCode(code string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("auth: hash code: %w", err)
	}
	return string(h), nil
}

// newSessionToken mints an opaque 256-bit token and its at-rest hash. The
// token is high-entropy, so SHA-256 (fast, indexable for future validation
// lookups) is the right at-rest hash — bcrypt would be both unnecessary and
// unindexable.
func newSessionToken() (token, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", "", fmt.Errorf("auth: generate session token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(buf)
	return token, hashToken(token), nil
}

// hashToken is the at-rest hash for an opaque session token (see
// newSessionToken); ValidateSession/RevokeSession hash an incoming token the
// same way to look it up.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ValidateSession looks up the session behind an opaque bearer token,
// rejecting a missing, revoked or expired one, and extends its rolling
// idle-expiry window on success. It satisfies platform/auth.SessionValidator.
func (s *Service) ValidateSession(ctx context.Context, token string) (platformauth.Principal, error) {
	sess, err := s.sessions.FindByTokenHash(ctx, hashToken(token))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return platformauth.Principal{}, ErrNoSession
		}
		return platformauth.Principal{}, fmt.Errorf("auth: validate session: find: %w", err)
	}

	now := s.clock.Now()
	if !sess.IsValid(now) {
		return platformauth.Principal{}, ErrNoSession
	}

	account, err := s.accounts.FindByID(ctx, sess.AccountID)
	if err != nil {
		return platformauth.Principal{}, fmt.Errorf("auth: validate session: find account: %w", err)
	}

	// Throttled: validation runs on every authenticated request, so writing the
	// rolling expiry each time would put an UPDATE on the hottest path.
	if sess.NeedsExtension(now) {
		sess.Extend(now)
		if err := s.sessions.Update(ctx, sess); err != nil {
			return platformauth.Principal{}, fmt.Errorf("auth: validate session: extend: %w", err)
		}
	}

	return platformauth.Principal{AccountID: account.ID.String(), Role: string(account.Role)}, nil
}

// Account looks up an account by id (used to render the full account payload
// after ValidateSession, which only returns the minimal platform Principal).
func (s *Service) Account(ctx context.Context, id uuid.UUID) (*Account, error) {
	account, err := s.accounts.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("auth: account: %w", err)
	}
	return account, nil
}

// RevokeSession signs out the current device only. Revoking an already-revoked
// or unknown token is a no-op, not an error — sign-out is idempotent.
func (s *Service) RevokeSession(ctx context.Context, token string) error {
	if err := s.sessions.RevokeByTokenHash(ctx, hashToken(token), s.clock.Now()); err != nil {
		return fmt.Errorf("auth: revoke session: %w", err)
	}
	return nil
}

// emitAuditEvent records an admin action that changed an account's auth state.
// Callers invoke it only for actions that had an effect: the table records
// effects, not attempts.
func (s *Service) emitAuditEvent(ctx context.Context, actorAdminID, action string, targetAccountID uuid.UUID, metadata map[string]any) error {
	event := &AuditEvent{
		ID:              uuid.New(),
		ActorAdminID:    actorAdminID,
		Action:          action,
		TargetAccountID: targetAccountID,
		CreatedAt:       s.clock.Now(),
		Metadata:        metadata,
	}
	if err := s.auditEvents.Create(ctx, event); err != nil {
		return fmt.Errorf("auth: emit audit event: %w", err)
	}
	return nil
}

// --- Admin operations. Handlers source actorAdminID from the server-verified
// AdminPrincipal, never from a request. ---

// defaultAccountListLimit applies when a caller passes limit <= 0, so a direct
// service call bypassing the HTTP layer's own default still returns rows.
const defaultAccountListLimit = 50

// AccountListItem is the admin account-list read model: richer than Account,
// never persisted itself.
type AccountListItem struct {
	ID            uuid.UUID
	Role          Role
	Phone         string
	GoogleLinked  bool
	SignupMethod  string
	CreatedAt     time.Time
	LockoutActive bool
}

// ListAccounts returns registered accounts, optionally filtered to phones
// containing phoneFilter, newest-created first. Lockout state comes from one
// batched lookup rather than a query per row.
func (s *Service) ListAccounts(ctx context.Context, phoneFilter string, limit, offset int) ([]AccountListItem, error) {
	if limit <= 0 {
		limit = defaultAccountListLimit
	}
	if offset < 0 {
		offset = 0
	}

	accounts, err := s.accounts.List(ctx, phoneFilter, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("auth: list accounts: %w", err)
	}

	phones := make([]string, 0, len(accounts))
	for _, a := range accounts {
		phones = append(phones, a.Phone)
	}
	locked, err := s.verifications.LockedPhones(ctx, phones, s.clock.Now())
	if err != nil {
		return nil, fmt.Errorf("auth: list accounts: locked phones: %w", err)
	}

	items := make([]AccountListItem, 0, len(accounts))
	for _, a := range accounts {
		items = append(items, AccountListItem{
			ID:            a.ID,
			Role:          a.Role,
			Phone:         a.Phone,
			GoogleLinked:  a.GoogleSubject != nil,
			SignupMethod:  signupMethod(a.GoogleSubject),
			CreatedAt:     a.CreatedAt,
			LockoutActive: locked[a.Phone],
		})
	}
	return items, nil
}

// signupMethod is derived from GoogleSubject alone: linking an account to
// Google after creation is out of scope, so a linked account was necessarily
// created through the Google-first signup path.
func signupMethod(googleSubject *string) string {
	if googleSubject != nil {
		return SignupMethodGoogle
	}
	return SignupMethodPhone
}

// ClearLockout lifts an active lockout on accountID's phone so the user can
// retry immediately. It targets the phone's most recently sent challenge, the
// only row that can hold a current lock since every fresh request creates a
// new one. An unlocked row is a no-op: cleared=false and no audit event.
//
// It resets sends_in_window alongside locked_until because the send-cap sums
// that field across a phone's rows — leaving a stale count would let the very
// next resend re-lock immediately and defeat the clear. The unlock and the
// audit write commit together.
func (s *Service) ClearLockout(ctx context.Context, actorAdminID string, accountID uuid.UUID) (bool, error) {
	account, err := s.accounts.FindByID(ctx, accountID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return false, ErrNotFound
		}
		return false, fmt.Errorf("auth: clear lockout: find account: %w", err)
	}

	now := s.clock.Now()
	v, err := s.verifications.FindLatestByPhone(ctx, account.Phone)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("auth: clear lockout: find latest verification: %w", err)
	}
	if !v.IsLocked(now) {
		return false, nil
	}

	v.LockedUntil = nil
	v.SendsInWindow = 0
	v.Status = StatusPending
	metadata := map[string]any{"verification_id": v.ID.String()}

	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.verifications.Update(ctx, v); err != nil {
			return fmt.Errorf("auth: clear lockout: update verification: %w", err)
		}
		return s.emitAuditEvent(ctx, actorAdminID, AuditActionClearLockout, accountID, metadata)
	}); err != nil {
		return false, err
	}
	return true, nil
}

// AnalyticsResult is the admin analytics read model.
type AnalyticsResult struct {
	// SignupsByMethod maps SignupMethodPhone/SignupMethodGoogle to the count of
	// accounts created in the selected range.
	SignupsByMethod map[string]int
	// VerificationFailureRate is defined in Analytics' doc comment.
	VerificationFailureRate float64
	// LockoutCount is the number of verification challenges that are currently
	// locked and were sent within the selected range.
	LockoutCount int
}

// rangeWindow resolves an analytics range query param to a duration. Only the
// three documented ranges are accepted.
func rangeWindow(rng string) (time.Duration, error) {
	switch rng {
	case "24h":
		return 24 * time.Hour, nil
	case "7d":
		return 7 * 24 * time.Hour, nil
	case "30d":
		return 30 * 24 * time.Hour, nil
	default:
		return 0, ErrInvalidRange
	}
}

// Analytics aggregates signup and authentication metrics over a selectable
// range, cut off from the injected Clock rather than the wall clock.
//
// Two figures here are approximations forced by the schema, and both are worth
// knowing before reading a dashboard built on them:
//
// verification_failure_rate is failed / (failed + verified) over challenges
// sent inside the range. There is no per-attempt log, only a cumulative
// counter per challenge, so the numerator counts wrong guesses from every
// in-range challenge while the denominator only adds those that ended
// verified. A range full of abandoned challenges therefore reads close to
// 100%. The formula is a judgment call and should be checked against what the
// product actually wants to measure.
//
// lockout_count anchors on sent_at because a challenge records no entered-lock
// timestamp, so one sent just before the boundary but locked well after is
// attributed to the range of its send.
func (s *Service) Analytics(ctx context.Context, rng string) (AnalyticsResult, error) {
	window, err := rangeWindow(rng)
	if err != nil {
		return AnalyticsResult{}, err
	}
	since := s.clock.Now().Add(-window)

	phoneOnly, googleLinked, err := s.accounts.CountCreatedSince(ctx, since)
	if err != nil {
		return AnalyticsResult{}, fmt.Errorf("auth: analytics: count accounts: %w", err)
	}

	failed, err := s.verifications.SumFailedAttemptsSince(ctx, since)
	if err != nil {
		return AnalyticsResult{}, fmt.Errorf("auth: analytics: sum failed attempts: %w", err)
	}
	verified, err := s.verifications.CountByStatusSince(ctx, StatusVerified, since)
	if err != nil {
		return AnalyticsResult{}, fmt.Errorf("auth: analytics: count verified: %w", err)
	}
	locked, err := s.verifications.CountByStatusSince(ctx, StatusLocked, since)
	if err != nil {
		return AnalyticsResult{}, fmt.Errorf("auth: analytics: count locked: %w", err)
	}

	var rate float64
	if total := failed + verified; total > 0 {
		rate = float64(failed) / float64(total)
	}

	return AnalyticsResult{
		SignupsByMethod: map[string]int{
			SignupMethodPhone:  phoneOnly,
			SignupMethodGoogle: googleLinked,
		},
		VerificationFailureRate: rate,
		LockoutCount:            locked,
	}, nil
}
