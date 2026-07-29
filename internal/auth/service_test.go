package auth_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/auth"
)

// --- per-package hand-written fakes (stdlib testing only, no testify) ---

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type fakeSender struct {
	fail      bool
	calls     int
	lastPhone string
	lastCode  string
}

func (s *fakeSender) SendCode(_ context.Context, phone, code string) error {
	s.calls++
	if s.fail {
		return errors.New("provider unavailable")
	}
	s.lastPhone = phone
	s.lastCode = code
	return nil
}

type fakeAccounts struct{ byPhone map[string]*auth.Account }

func newFakeAccounts() *fakeAccounts { return &fakeAccounts{byPhone: map[string]*auth.Account{}} }

func (r *fakeAccounts) FindByPhone(_ context.Context, phone string) (*auth.Account, error) {
	if a, ok := r.byPhone[phone]; ok {
		return a, nil
	}
	return nil, auth.ErrNotFound
}

func (r *fakeAccounts) Create(_ context.Context, a *auth.Account) error {
	if _, ok := r.byPhone[a.Phone]; ok {
		return auth.ErrAccountExists
	}
	cp := *a
	r.byPhone[a.Phone] = &cp
	return nil
}

func (r *fakeAccounts) FindByID(_ context.Context, id uuid.UUID) (*auth.Account, error) {
	for _, a := range r.byPhone {
		if a.ID == id {
			return a, nil
		}
	}
	return nil, auth.ErrNotFound
}

func (r *fakeAccounts) FindByGoogleSubject(_ context.Context, subject string) (*auth.Account, error) {
	for _, a := range r.byPhone {
		if a.GoogleSubject != nil && *a.GoogleSubject == subject {
			return a, nil
		}
	}
	return nil, auth.ErrNotFound
}

// List mirrors the postgres LIKE '%'||$1||'%' filter (empty matches all) and
// the newest-created-first ordering.
func (r *fakeAccounts) List(_ context.Context, phoneFilter string, limit, offset int) ([]auth.Account, error) {
	matched := make([]auth.Account, 0, len(r.byPhone))
	for _, a := range r.byPhone {
		if phoneFilter == "" || strings.Contains(a.Phone, phoneFilter) {
			matched = append(matched, *a)
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].CreatedAt.After(matched[j].CreatedAt) })

	if offset >= len(matched) {
		return []auth.Account{}, nil
	}
	end := offset + limit
	if limit <= 0 || end > len(matched) {
		end = len(matched)
	}
	return matched[offset:end], nil
}

// CountCreatedSince mirrors the postgres FILTER-based split.
func (r *fakeAccounts) CountCreatedSince(_ context.Context, since time.Time) (phoneOnly, googleLinked int, err error) {
	for _, a := range r.byPhone {
		if a.CreatedAt.Before(since) {
			continue
		}
		if a.GoogleSubject != nil {
			googleLinked++
		} else {
			phoneOnly++
		}
	}
	return phoneOnly, googleLinked, nil
}

// fakeVerifications stores copies and hands back copies, so the service must
// call Update to persist — mirroring real database semantics.
type fakeVerifications struct {
	byID              map[uuid.UUID]*auth.Verification
	lockedPhonesCalls int
}

func newFakeVerifications() *fakeVerifications {
	return &fakeVerifications{byID: map[uuid.UUID]*auth.Verification{}}
}

func (r *fakeVerifications) Create(_ context.Context, v *auth.Verification) error {
	cp := *v
	r.byID[v.ID] = &cp
	return nil
}

func (r *fakeVerifications) Get(_ context.Context, id uuid.UUID) (*auth.Verification, error) {
	v, ok := r.byID[id]
	if !ok {
		return nil, auth.ErrNotFound
	}
	cp := *v
	return &cp, nil
}

func (r *fakeVerifications) Update(_ context.Context, v *auth.Verification) error {
	if _, ok := r.byID[v.ID]; !ok {
		return auth.ErrNotFound
	}
	cp := *v
	r.byID[v.ID] = &cp
	return nil
}

// CountSendsSince sums sends_in_window across the phone's rows whose last send
// lands at or after since, mirroring the postgres SUM query.
func (r *fakeVerifications) CountSendsSince(_ context.Context, phone string, since time.Time) (int, error) {
	total := 0
	for _, v := range r.byID {
		if v.Phone == phone && !v.SentAt.Before(since) {
			total += v.SendsInWindow
		}
	}
	return total, nil
}

// FindLatestByPhone returns a copy of the phone's most recently sent row, or
// ErrNotFound if it has none.
func (r *fakeVerifications) FindLatestByPhone(_ context.Context, phone string) (*auth.Verification, error) {
	var latest *auth.Verification
	for _, v := range r.byID {
		if v.Phone != phone {
			continue
		}
		if latest == nil || v.SentAt.After(latest.SentAt) {
			latest = v
		}
	}
	if latest == nil {
		return nil, auth.ErrNotFound
	}
	cp := *latest
	return &cp, nil
}

// LockedPhones mirrors the postgres batched lookup: each phone's most recently
// sent row, reduced to whether it is locked at now. It also counts its calls
// so a test can prove the admin list does not fall back to one query per row.
func (r *fakeVerifications) LockedPhones(ctx context.Context, phones []string, now time.Time) (map[string]bool, error) {
	r.lockedPhonesCalls++
	locked := make(map[string]bool, len(phones))
	for _, phone := range phones {
		v, err := r.FindLatestByPhone(ctx, phone)
		if err != nil {
			if errors.Is(err, auth.ErrNotFound) {
				continue
			}
			return nil, err
		}
		locked[phone] = v.IsLocked(now)
	}
	return locked, nil
}

// SumFailedAttemptsSince mirrors the postgres SUM query.
func (r *fakeVerifications) SumFailedAttemptsSince(_ context.Context, since time.Time) (int, error) {
	total := 0
	for _, v := range r.byID {
		if !v.SentAt.Before(since) {
			total += v.FailedAttempts
		}
	}
	return total, nil
}

// CountByStatusSince mirrors the postgres COUNT query.
func (r *fakeVerifications) CountByStatusSince(_ context.Context, status auth.Status, since time.Time) (int, error) {
	n := 0
	for _, v := range r.byID {
		if v.Status == status && !v.SentAt.Before(since) {
			n++
		}
	}
	return n, nil
}

var errSessionCreate = errors.New("session create failed")

type fakeSessions struct {
	created []*auth.Session
	byHash  map[string]*auth.Session
	fail    bool
	updates int
}

func (r *fakeSessions) Create(_ context.Context, s *auth.Session) error {
	if r.fail {
		return errSessionCreate
	}
	cp := *s
	r.created = append(r.created, &cp)
	if r.byHash == nil {
		r.byHash = map[string]*auth.Session{}
	}
	r.byHash[s.TokenHash] = &cp
	return nil
}

func (r *fakeSessions) FindByTokenHash(_ context.Context, tokenHash string) (*auth.Session, error) {
	s, ok := r.byHash[tokenHash]
	if !ok {
		return nil, auth.ErrNotFound
	}
	cp := *s
	return &cp, nil
}

func (r *fakeSessions) Update(_ context.Context, s *auth.Session) error {
	if _, ok := r.byHash[s.TokenHash]; !ok {
		return auth.ErrNotFound
	}
	r.updates++
	cp := *s
	r.byHash[s.TokenHash] = &cp
	return nil
}

func (r *fakeSessions) RevokeByTokenHash(_ context.Context, tokenHash string, revokedAt time.Time) error {
	s, ok := r.byHash[tokenHash]
	if !ok || s.RevokedAt != nil {
		return nil
	}
	cp := *s
	cp.RevokedAt = &revokedAt
	r.byHash[tokenHash] = &cp
	return nil
}

// fakeIdentityVerifier maps a fixed set of ID tokens to identities; anything
// else fails, standing in for the real Google ID-token verifier.
type fakeIdentityVerifier struct {
	identities map[string]*auth.GoogleIdentity
}

func newFakeIdentityVerifier() *fakeIdentityVerifier {
	return &fakeIdentityVerifier{identities: map[string]*auth.GoogleIdentity{}}
}

func (v *fakeIdentityVerifier) Verify(_ context.Context, idToken string) (*auth.GoogleIdentity, error) {
	id, ok := v.identities[idToken]
	if !ok {
		return nil, auth.ErrGoogleFailed
	}
	return id, nil
}

type fakeAuditEvents struct{ created []*auth.AuditEvent }

func (r *fakeAuditEvents) Create(_ context.Context, e *auth.AuditEvent) error {
	cp := *e
	r.created = append(r.created, &cp)
	return nil
}

// fakeTx is an in-memory TxRunner: it runs the closure directly and returns
// its error. It cannot exercise real rollback (the repos are independent fakes
// with no shared transaction), so tests using it assert the failure path
// aborts at the right step; production's db.TxManager provides the actual
// rollback.
type fakeTx struct{ calls int }

func (t *fakeTx) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	t.calls++
	return fn(ctx)
}

type harness struct {
	clock    *fakeClock
	sender   *fakeSender
	identity *fakeIdentityVerifier
	accounts *fakeAccounts
	verifs   *fakeVerifications
	sessions *fakeSessions
	audit    *fakeAuditEvents
	tx       *fakeTx
	svc      *auth.Service
}

func newHarness() *harness {
	h := &harness{
		clock:    &fakeClock{now: base},
		sender:   &fakeSender{},
		identity: newFakeIdentityVerifier(),
		accounts: newFakeAccounts(),
		verifs:   newFakeVerifications(),
		sessions: &fakeSessions{},
		audit:    &fakeAuditEvents{},
		tx:       &fakeTx{},
	}
	h.svc = auth.NewService(h.clock, h.sender, h.identity, h.accounts, h.verifs, h.sessions, h.audit, h.tx, testLinkSecret)
	return h
}

// testLinkSecret is a fixed signing secret for the pending-Google-link token
// (google.go) — deterministic is fine here, it's per-test-process, not shared
// with anything a real client could observe or influence.
var testLinkSecret = []byte("test-only-google-link-secret-not-for-prod")

const rawPhone = "01012345678"

// reqParams builds a signup-shaped RequestVerificationParams, the common case
// across most tests; signin/Google-linking tests build the struct literal
// directly for their extra fields.
func reqParams(intent auth.Intent, phone string, role auth.Role, language auth.Language) auth.RequestVerificationParams {
	return auth.RequestVerificationParams{Intent: intent, Phone: phone, Role: role, Language: language}
}

func wrongCodeFor(sent string) string {
	if sent == "000000" {
		return "111111"
	}
	return "000000"
}

// --- tests ---

func TestConfirmCreatesAccountOnlyAfterCorrectCode(t *testing.T) {
	ctx := context.Background()
	h := newHarness()

	v, err := h.svc.RequestVerification(ctx, reqParams(auth.IntentSignup, rawPhone, auth.RoleCoach, auth.LanguageArabic))
	if err != nil {
		t.Fatalf("RequestVerification: %v", err)
	}
	if h.sender.calls != 1 {
		t.Fatalf("sender calls = %d, want 1", h.sender.calls)
	}
	if len(h.accounts.byPhone) != 0 {
		t.Fatal("account must not exist before confirm")
	}

	res, err := h.svc.ConfirmVerification(ctx, v.ID, h.sender.lastCode)
	if err != nil {
		t.Fatalf("ConfirmVerification: %v", err)
	}
	if res.SessionToken == "" {
		t.Fatal("expected a session token")
	}
	if res.Route != "onboarding" {
		t.Fatalf("route = %q, want onboarding", res.Route)
	}
	if res.Account.Role != auth.RoleCoach || res.Account.PreferredLanguage != auth.LanguageArabic {
		t.Fatalf("account role/lang = %q/%q", res.Account.Role, res.Account.PreferredLanguage)
	}
	if res.Account.Phone != "+201012345678" {
		t.Fatalf("account phone = %q, want normalized", res.Account.Phone)
	}
	if len(h.accounts.byPhone) != 1 || len(h.sessions.created) != 1 {
		t.Fatalf("accounts=%d sessions=%d, want 1/1", len(h.accounts.byPhone), len(h.sessions.created))
	}
}

func TestConfirmWrongCodeLocksAfterFiveAttempts(t *testing.T) {
	ctx := context.Background()
	h := newHarness()

	v, err := h.svc.RequestVerification(ctx, reqParams(auth.IntentSignup, rawPhone, auth.RoleAthlete, auth.LanguageEnglish))
	if err != nil {
		t.Fatalf("RequestVerification: %v", err)
	}
	wrong := wrongCodeFor(h.sender.lastCode)

	for attempt := 1; attempt <= auth.MaxFailedAttempts-1; attempt++ {
		_, err := h.svc.ConfirmVerification(ctx, v.ID, wrong)
		var inc *auth.IncorrectCodeError
		if !errors.As(err, &inc) {
			t.Fatalf("attempt %d error = %v, want *IncorrectCodeError", attempt, err)
		}
		if want := auth.MaxFailedAttempts - attempt; inc.AttemptsRemaining != want {
			t.Fatalf("attempt %d AttemptsRemaining = %d, want %d", attempt, inc.AttemptsRemaining, want)
		}
	}

	_, err = h.svc.ConfirmVerification(ctx, v.ID, wrong)
	if !errors.Is(err, auth.ErrLocked) {
		t.Fatalf("fifth attempt error = %v, want ErrLocked", err)
	}
	var locked *auth.LockedError
	if !errors.As(err, &locked) {
		t.Fatalf("fifth attempt error = %v, want *LockedError", err)
	}
	if !locked.LockedUntil.Equal(base.Add(auth.LockoutDuration)) {
		t.Fatalf("LockedUntil = %v, want %v", locked.LockedUntil, base.Add(auth.LockoutDuration))
	}
	if len(h.accounts.byPhone) != 0 {
		t.Fatal("no account should be created on a locked challenge")
	}
}

func TestConfirmExpiredViaClock(t *testing.T) {
	ctx := context.Background()
	h := newHarness()

	v, err := h.svc.RequestVerification(ctx, reqParams(auth.IntentSignup, rawPhone, auth.RoleCoach, auth.LanguageArabic))
	if err != nil {
		t.Fatalf("RequestVerification: %v", err)
	}

	h.clock.now = base.Add(auth.OTPTTL + time.Minute)
	if _, err := h.svc.ConfirmVerification(ctx, v.ID, h.sender.lastCode); !errors.Is(err, auth.ErrExpired) {
		t.Fatalf("confirm after expiry error = %v, want ErrExpired", err)
	}
}

func TestRequestDuplicatePhoneBlocksBeforeSend(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	h.accounts.byPhone["+201012345678"] = &auth.Account{Phone: "+201012345678", Role: auth.RoleCoach}

	_, err := h.svc.RequestVerification(ctx, reqParams(auth.IntentSignup, rawPhone, auth.RoleCoach, auth.LanguageArabic))
	if !errors.Is(err, auth.ErrAccountExists) {
		t.Fatalf("error = %v, want ErrAccountExists", err)
	}
	if h.sender.calls != 0 {
		t.Fatalf("sender calls = %d, want 0 (no send before the duplicate check clears)", h.sender.calls)
	}
}

func TestResendCooldown(t *testing.T) {
	ctx := context.Background()
	h := newHarness()

	v, err := h.svc.RequestVerification(ctx, reqParams(auth.IntentSignup, rawPhone, auth.RoleCoach, auth.LanguageArabic))
	if err != nil {
		t.Fatalf("RequestVerification: %v", err)
	}

	if _, err := h.svc.ResendVerification(ctx, v.ID); !errors.Is(err, auth.ErrResendCooldown) {
		t.Fatalf("immediate resend error = %v, want ErrResendCooldown", err)
	}

	h.clock.now = base.Add(auth.ResendCooldown)
	if _, err := h.svc.ResendVerification(ctx, v.ID); err != nil {
		t.Fatalf("resend after cooldown: %v", err)
	}
	if h.sender.calls != 2 {
		t.Fatalf("sender calls = %d, want 2 (request + resend)", h.sender.calls)
	}
}

func TestSendCapLocks(t *testing.T) {
	ctx := context.Background()
	h := newHarness()

	v, err := h.svc.RequestVerification(ctx, reqParams(auth.IntentSignup, rawPhone, auth.RoleCoach, auth.LanguageArabic))
	if err != nil {
		t.Fatalf("RequestVerification: %v", err)
	}

	now := base
	// Four successful resends bring the window to the 5-send cap.
	for i := 1; i <= auth.MaxSendsPerWindow-1; i++ {
		now = now.Add(auth.ResendCooldown)
		h.clock.now = now
		if _, err := h.svc.ResendVerification(ctx, v.ID); err != nil {
			t.Fatalf("resend %d: %v", i, err)
		}
	}

	now = now.Add(auth.ResendCooldown)
	h.clock.now = now
	if _, err := h.svc.ResendVerification(ctx, v.ID); !errors.Is(err, auth.ErrLocked) {
		t.Fatalf("resend past cap error = %v, want ErrLocked", err)
	}
	if h.sender.calls != auth.MaxSendsPerWindow {
		t.Fatalf("sender calls = %d, want %d (the capped send never dispatches)", h.sender.calls, auth.MaxSendsPerWindow)
	}
}

func TestSendFailureNotCountedThenRetrySucceeds(t *testing.T) {
	ctx := context.Background()
	h := newHarness()

	h.sender.fail = true
	if _, err := h.svc.RequestVerification(ctx, reqParams(auth.IntentSignup, rawPhone, auth.RoleCoach, auth.LanguageArabic)); !errors.Is(err, auth.ErrSendFailed) {
		t.Fatalf("error = %v, want ErrSendFailed", err)
	}
	if len(h.verifs.byID) != 0 {
		t.Fatal("a failed send must persist no challenge")
	}

	h.sender.fail = false
	v, err := h.svc.RequestVerification(ctx, reqParams(auth.IntentSignup, rawPhone, auth.RoleCoach, auth.LanguageArabic))
	if err != nil {
		t.Fatalf("retry RequestVerification: %v", err)
	}
	if _, err := h.svc.ConfirmVerification(ctx, v.ID, h.sender.lastCode); err != nil {
		t.Fatalf("confirm after retry: %v", err)
	}
	if len(h.accounts.byPhone) != 1 {
		t.Fatalf("accounts = %d, want 1", len(h.accounts.byPhone))
	}
}

func TestRequestUnsupportedIntent(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	if _, err := h.svc.RequestVerification(ctx, reqParams(auth.Intent("bogus"), rawPhone, auth.RoleCoach, auth.LanguageArabic)); !errors.Is(err, auth.ErrUnsupportedIntent) {
		t.Fatalf("error = %v, want ErrUnsupportedIntent", err)
	}
}

// TestConfirmSessionCreateFailureAbortsBeforeVerify proves the confirm writes
// run inside InTx and that a session-create failure aborts before the
// challenge is marked verified. The in-memory TxRunner has no real rollback,
// so the account left in the fake reflects that limitation; production's
// db.TxManager rolls the account insert back on the same error.
func TestConfirmSessionCreateFailureAbortsBeforeVerify(t *testing.T) {
	ctx := context.Background()
	h := newHarness()

	v, err := h.svc.RequestVerification(ctx, reqParams(auth.IntentSignup, rawPhone, auth.RoleCoach, auth.LanguageArabic))
	if err != nil {
		t.Fatalf("RequestVerification: %v", err)
	}

	h.sessions.fail = true
	if _, err := h.svc.ConfirmVerification(ctx, v.ID, h.sender.lastCode); !errors.Is(err, errSessionCreate) {
		t.Fatalf("confirm error = %v, want wrapped errSessionCreate", err)
	}
	if h.tx.calls != 1 {
		t.Fatalf("tx.calls = %d, want 1 (account/session/verify writes must run inside InTx)", h.tx.calls)
	}

	got, gerr := h.verifs.Get(ctx, v.ID)
	if gerr != nil {
		t.Fatalf("get verification: %v", gerr)
	}
	if got.Status != auth.StatusPending {
		t.Fatalf("verification status = %q, want pending (not verified)", got.Status)
	}
	if len(h.sessions.created) != 0 {
		t.Fatalf("sessions created = %d, want 0", len(h.sessions.created))
	}
}

// TestRequestSendCapLocksAfterFiveSends drives the send-cap on the request
// endpoint itself: five sends (spaced past the cooldown) then a sixth request
// locks.
func TestRequestSendCapLocksAfterFiveSends(t *testing.T) {
	ctx := context.Background()
	h := newHarness()

	now := base
	for i := 1; i <= auth.MaxSendsPerWindow; i++ {
		h.clock.now = now
		if _, err := h.svc.RequestVerification(ctx, reqParams(auth.IntentSignup, rawPhone, auth.RoleCoach, auth.LanguageArabic)); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		now = now.Add(auth.ResendCooldown)
	}

	h.clock.now = now
	_, err := h.svc.RequestVerification(ctx, reqParams(auth.IntentSignup, rawPhone, auth.RoleCoach, auth.LanguageArabic))
	if !errors.Is(err, auth.ErrLocked) {
		t.Fatalf("sixth request error = %v, want ErrLocked", err)
	}
	var locked *auth.LockedError
	if !errors.As(err, &locked) {
		t.Fatalf("sixth request error = %v, want *LockedError", err)
	}
	if h.sender.calls != auth.MaxSendsPerWindow {
		t.Fatalf("sender calls = %d, want %d (the capped request never sends)", h.sender.calls, auth.MaxSendsPerWindow)
	}
}

// TestRequestVerificationCannotBypassLockoutViaFreshRequest proves a phone
// locked by 5 wrong-code attempts stays locked against a brand-new
// RequestVerification call too — not just against ResendVerification/
// ConfirmVerification on the same challenge. Before this test, a caller could
// route around an active lockout simply by starting a fresh signup/signin
// request instead of resending, since RequestVerification never checked the
// phone's latest challenge for IsLocked.
func TestRequestVerificationCannotBypassLockoutViaFreshRequest(t *testing.T) {
	ctx := context.Background()
	h := newHarness()

	v, err := h.svc.RequestVerification(ctx, reqParams(auth.IntentSignup, rawPhone, auth.RoleCoach, auth.LanguageArabic))
	if err != nil {
		t.Fatalf("RequestVerification: %v", err)
	}
	wrong := wrongCodeFor(h.sender.lastCode)
	for i := 0; i < auth.MaxFailedAttempts; i++ {
		_, _ = h.svc.ConfirmVerification(ctx, v.ID, wrong)
	}

	sendsBefore := h.sender.calls
	_, err = h.svc.RequestVerification(ctx, reqParams(auth.IntentSignup, rawPhone, auth.RoleCoach, auth.LanguageArabic))
	var locked *auth.LockedError
	if !errors.As(err, &locked) {
		t.Fatalf("fresh request on a locked phone error = %v, want *LockedError", err)
	}
	if h.sender.calls != sendsBefore {
		t.Fatalf("sender calls = %d, want %d (a locked phone must not receive a new code)", h.sender.calls, sendsBefore)
	}
}

// TestRequestVerificationSendCapPersistsLock proves a send-cap trip inside
// RequestVerification itself persists the lock on the phone's latest row — not
// just returns a LockedError to the caller — so the lockout is later
// observable and clearable by an admin, exactly like a lockout reached via
// ResendVerification or five wrong codes.
func TestRequestVerificationSendCapPersistsLock(t *testing.T) {
	ctx := context.Background()
	h := newHarness()

	now := base
	for i := 1; i <= auth.MaxSendsPerWindow; i++ {
		h.clock.now = now
		if _, err := h.svc.RequestVerification(ctx, reqParams(auth.IntentSignup, rawPhone, auth.RoleCoach, auth.LanguageArabic)); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		now = now.Add(auth.ResendCooldown)
	}

	h.clock.now = now
	if _, err := h.svc.RequestVerification(ctx, reqParams(auth.IntentSignup, rawPhone, auth.RoleCoach, auth.LanguageArabic)); err == nil {
		t.Fatal("expected the sixth request to be locked")
	}

	latest, err := h.verifs.FindLatestByPhone(ctx, "+201012345678")
	if err != nil {
		t.Fatalf("FindLatestByPhone: %v", err)
	}
	if !latest.IsLocked(now) {
		t.Fatal("expected the persisted verification row to be locked")
	}
}

// signUpAndConfirm drives a fresh phone signup to completion and returns the
// issued session token plus the account id, for tests that need a real session
// to validate/revoke.
func signUpAndConfirm(t *testing.T, h *harness) (token string, accountID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	v, err := h.svc.RequestVerification(ctx, reqParams(auth.IntentSignup, rawPhone, auth.RoleCoach, auth.LanguageArabic))
	if err != nil {
		t.Fatalf("RequestVerification: %v", err)
	}
	res, err := h.svc.ConfirmVerification(ctx, v.ID, h.sender.lastCode)
	if err != nil {
		t.Fatalf("ConfirmVerification: %v", err)
	}
	return res.SessionToken, res.Account.ID
}

func TestValidateSessionSucceedsAndExtendsIdleWindow(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	token, accountID := signUpAndConfirm(t, h)

	principal, err := h.svc.ValidateSession(ctx, token)
	if err != nil {
		t.Fatalf("ValidateSession: %v", err)
	}
	if principal.AccountID != accountID.String() {
		t.Fatalf("principal.AccountID = %q, want %q", principal.AccountID, accountID.String())
	}
	if principal.Role != string(auth.RoleCoach) {
		t.Fatalf("principal.Role = %q, want %q", principal.Role, auth.RoleCoach)
	}

	var stored *auth.Session
	for _, s := range h.sessions.byHash {
		stored = s
	}
	if !stored.ExpiresAt.Equal(base.Add(auth.SessionIdleWindow)) {
		t.Fatalf("session ExpiresAt = %v, want rolled forward to %v", stored.ExpiresAt, base.Add(auth.SessionIdleWindow))
	}
}

// Validation runs on every authenticated request, so the rolling-expiry write
// must not.
func TestValidateSessionSkipsExpiryWriteWithinInterval(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	token, _ := signUpAndConfirm(t, h)
	h.sessions.updates = 0

	h.clock.now = base.Add(auth.SessionExtendInterval - time.Minute)
	for range 5 {
		if _, err := h.svc.ValidateSession(ctx, token); err != nil {
			t.Fatalf("ValidateSession: %v", err)
		}
	}

	if h.sessions.updates != 0 {
		t.Fatalf("session updates = %d, want 0 within the extend interval", h.sessions.updates)
	}
}

func TestValidateSessionWritesExpiryOncePastInterval(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	token, _ := signUpAndConfirm(t, h)
	h.sessions.updates = 0

	past := base.Add(auth.SessionExtendInterval)
	h.clock.now = past
	if _, err := h.svc.ValidateSession(ctx, token); err != nil {
		t.Fatalf("ValidateSession: %v", err)
	}
	if h.sessions.updates != 1 {
		t.Fatalf("session updates = %d, want 1 once the interval has elapsed", h.sessions.updates)
	}

	var stored *auth.Session
	for _, s := range h.sessions.byHash {
		stored = s
	}
	if !stored.ExpiresAt.Equal(past.Add(auth.SessionIdleWindow)) {
		t.Fatalf("session ExpiresAt = %v, want %v", stored.ExpiresAt, past.Add(auth.SessionIdleWindow))
	}

	// The write reset LastActiveAt, so the throttle applies again from here.
	h.clock.now = past.Add(time.Minute)
	if _, err := h.svc.ValidateSession(ctx, token); err != nil {
		t.Fatalf("ValidateSession: %v", err)
	}
	if h.sessions.updates != 1 {
		t.Fatalf("session updates = %d, want the interval to restart after a write", h.sessions.updates)
	}
}

func TestValidateSessionRejectsExpiredIdleSession(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	token, _ := signUpAndConfirm(t, h)

	h.clock.now = base.Add(auth.SessionIdleWindow + time.Hour)
	if _, err := h.svc.ValidateSession(ctx, token); !errors.Is(err, auth.ErrNoSession) {
		t.Fatalf("error = %v, want ErrNoSession", err)
	}
}

func TestValidateSessionRejectsAfterSignOut(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	token, _ := signUpAndConfirm(t, h)

	if err := h.svc.RevokeSession(ctx, token); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	if _, err := h.svc.ValidateSession(ctx, token); !errors.Is(err, auth.ErrNoSession) {
		t.Fatalf("error after sign-out = %v, want ErrNoSession", err)
	}
}

// TestRevokeSessionIsIdempotent proves signing out twice (or a token that
// never existed) is not an error — sign-out is idempotent.
func TestRevokeSessionIsIdempotent(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	token, _ := signUpAndConfirm(t, h)

	if err := h.svc.RevokeSession(ctx, token); err != nil {
		t.Fatalf("first RevokeSession: %v", err)
	}
	if err := h.svc.RevokeSession(ctx, token); err != nil {
		t.Fatalf("second RevokeSession: %v", err)
	}
	if err := h.svc.RevokeSession(ctx, "never-issued"); err != nil {
		t.Fatalf("RevokeSession on unknown token: %v", err)
	}
}

func TestValidateSessionUnknownTokenRejected(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	if _, err := h.svc.ValidateSession(ctx, "not-a-real-token"); !errors.Is(err, auth.ErrNoSession) {
		t.Fatalf("error = %v, want ErrNoSession", err)
	}
}

// TestRequestCooldownBlocksRepeat proves a repeat request inside the 60-s
// cooldown is rejected before any code is sent.
func TestRequestCooldownBlocksRepeat(t *testing.T) {
	ctx := context.Background()
	h := newHarness()

	if _, err := h.svc.RequestVerification(ctx, reqParams(auth.IntentSignup, rawPhone, auth.RoleCoach, auth.LanguageArabic)); err != nil {
		t.Fatalf("first request: %v", err)
	}

	h.clock.now = base.Add(30 * time.Second) // inside the 60-s cooldown
	if _, err := h.svc.RequestVerification(ctx, reqParams(auth.IntentSignup, rawPhone, auth.RoleCoach, auth.LanguageArabic)); !errors.Is(err, auth.ErrResendCooldown) {
		t.Fatalf("repeat request within cooldown error = %v, want ErrResendCooldown", err)
	}
	if h.sender.calls != 1 {
		t.Fatalf("sender calls = %d, want 1 (cooldown blocks before the send)", h.sender.calls)
	}
}

// --- returning sign-in ---

// TestRequestSigninNoAccountBlocksBeforeSend proves signin on an unregistered
// phone fails with ErrNoAccount before any code is sent.
func TestRequestSigninNoAccountBlocksBeforeSend(t *testing.T) {
	ctx := context.Background()
	h := newHarness()

	_, err := h.svc.RequestVerification(ctx, auth.RequestVerificationParams{
		Intent: auth.IntentSignin,
		Phone:  rawPhone,
	})
	if !errors.Is(err, auth.ErrNoAccount) {
		t.Fatalf("error = %v, want ErrNoAccount", err)
	}
	if h.sender.calls != 0 {
		t.Fatalf("sender calls = %d, want 0 (no send before the no-account check clears)", h.sender.calls)
	}
}

// TestSigninExistingAccountResumesSession proves signin on a registered phone
// issues a session for the existing account — no new account row — and routes
// home.
func TestSigninExistingAccountResumesSession(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	accountID := uuid.New()
	h.accounts.byPhone["+201012345678"] = &auth.Account{
		ID: accountID, Phone: "+201012345678", Role: auth.RoleAthlete, PreferredLanguage: auth.LanguageEnglish,
	}

	v, err := h.svc.RequestVerification(ctx, auth.RequestVerificationParams{
		Intent: auth.IntentSignin,
		Phone:  rawPhone,
	})
	if err != nil {
		t.Fatalf("RequestVerification: %v", err)
	}

	res, err := h.svc.ConfirmVerification(ctx, v.ID, h.sender.lastCode)
	if err != nil {
		t.Fatalf("ConfirmVerification: %v", err)
	}
	if res.Route != auth.RouteHome {
		t.Fatalf("route = %q, want home", res.Route)
	}
	if res.Account.ID != accountID {
		t.Fatalf("account id = %v, want existing account %v", res.Account.ID, accountID)
	}
	if len(h.accounts.byPhone) != 1 {
		t.Fatalf("accounts = %d, want 1 (no new account created)", len(h.accounts.byPhone))
	}
	if len(h.sessions.created) != 1 {
		t.Fatalf("sessions created = %d, want 1", len(h.sessions.created))
	}
}

// TestRevokeSessionOnlyAffectsCurrentDevice proves sign-out ends only the
// device's own session; a concurrent session for the same account survives.
func TestRevokeSessionOnlyAffectsCurrentDevice(t *testing.T) {
	ctx := context.Background()
	h := newHarness()

	token1, accountID := signUpAndConfirm(t, h)

	// Past the resend cooldown, so the signin request below isn't blocked by the
	// signup challenge just sent for the same phone.
	h.clock.now = base.Add(auth.ResendCooldown)

	v, err := h.svc.RequestVerification(ctx, auth.RequestVerificationParams{
		Intent: auth.IntentSignin,
		Phone:  rawPhone,
	})
	if err != nil {
		t.Fatalf("signin RequestVerification: %v", err)
	}
	res, err := h.svc.ConfirmVerification(ctx, v.ID, h.sender.lastCode)
	if err != nil {
		t.Fatalf("signin ConfirmVerification: %v", err)
	}
	token2 := res.SessionToken
	if res.Account.ID != accountID {
		t.Fatalf("signin resumed a different account: got %v, want %v", res.Account.ID, accountID)
	}
	if len(h.sessions.created) != 2 {
		t.Fatalf("sessions created = %d, want 2", len(h.sessions.created))
	}

	if err := h.svc.RevokeSession(ctx, token1); err != nil {
		t.Fatalf("RevokeSession(token1): %v", err)
	}

	if _, err := h.svc.ValidateSession(ctx, token1); !errors.Is(err, auth.ErrNoSession) {
		t.Fatalf("token1 after revoke = %v, want ErrNoSession", err)
	}
	if _, err := h.svc.ValidateSession(ctx, token2); err != nil {
		t.Fatalf("token2 should remain valid after revoking token1: %v", err)
	}
}

// --- Google exchange ---

// TestGoogleExchangeExistingAccountResumesSession proves a known Google
// identity resumes the linked account's session with no OTP step.
func TestGoogleExchangeExistingAccountResumesSession(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	subject := "g-sub-existing"
	accountID := uuid.New()
	h.accounts.byPhone["+201099999999"] = &auth.Account{
		ID: accountID, Phone: "+201099999999", Role: auth.RoleAthlete,
		GoogleSubject: &subject, PreferredLanguage: auth.LanguageEnglish,
	}
	h.identity.identities["good-token"] = &auth.GoogleIdentity{Subject: subject, Email: "existing@example.com", EmailVerified: true}

	res, err := h.svc.GoogleExchange(ctx, "good-token", nil)
	if err != nil {
		t.Fatalf("GoogleExchange: %v", err)
	}
	if res.NoAccount {
		t.Fatal("expected an existing-account result")
	}
	if res.SessionToken == "" {
		t.Fatal("expected a session token")
	}
	if res.Route != auth.RouteHome {
		t.Fatalf("route = %q, want home", res.Route)
	}
	if res.Account.ID != accountID {
		t.Fatalf("account id = %v, want %v", res.Account.ID, accountID)
	}
	if len(h.sessions.created) != 1 {
		t.Fatalf("sessions created = %d, want 1", len(h.sessions.created))
	}
}

// TestGoogleExchangeUnknownIdentityRoutesToSignup proves an unrecognized
// Google subject reports NoAccount with no side effects, so the client routes
// into the Google-signup phone step.
func TestGoogleExchangeUnknownIdentityRoutesToSignup(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	h.identity.identities["good-token"] = &auth.GoogleIdentity{Subject: "g-sub-unknown", Email: "new@example.com", EmailVerified: true}

	res, err := h.svc.GoogleExchange(ctx, "good-token", nil)
	if err != nil {
		t.Fatalf("GoogleExchange: %v", err)
	}
	if !res.NoAccount {
		t.Fatal("expected NoAccount = true")
	}
	if res.PhonePrefill != nil {
		t.Fatal("phone_prefill must stay nil (GoogleIdentity carries no phone)")
	}
	if len(h.sessions.created) != 0 {
		t.Fatal("no session should be created for an unknown identity")
	}
}

// TestGoogleExchangeRejectedTokenFails proves a token the identity verifier
// rejects surfaces ErrGoogleFailed.
func TestGoogleExchangeRejectedTokenFails(t *testing.T) {
	ctx := context.Background()
	h := newHarness()

	if _, err := h.svc.GoogleExchange(ctx, "unknown-token", nil); !errors.Is(err, auth.ErrGoogleFailed) {
		t.Fatalf("error = %v, want ErrGoogleFailed", err)
	}
}

// TestGoogleFirstSignupLinksIdentity drives the full Google-first path: an
// unknown Google identity routes to verify_phone with a signed pending-link
// token, and completing the phone verification with that token creates the
// account already linked to Google. The test deliberately never fabricates a
// raw subject/email for RequestVerification — the token is the only thing that
// may authorize a link, exactly like a real client.
func TestGoogleFirstSignupLinksIdentity(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	subject := "g-sub-signup"
	email := "signup@example.com"
	h.identity.identities["good-token"] = &auth.GoogleIdentity{Subject: subject, Email: email, EmailVerified: true}

	exchangeRes, err := h.svc.GoogleExchange(ctx, "good-token", nil)
	if err != nil {
		t.Fatalf("GoogleExchange: %v", err)
	}
	if !exchangeRes.NoAccount {
		t.Fatal("expected NoAccount = true for a first-time Google identity")
	}
	if exchangeRes.PendingLinkToken == "" {
		t.Fatal("expected a non-empty PendingLinkToken for a first-time Google identity")
	}

	linkToken := exchangeRes.PendingLinkToken
	v, err := h.svc.RequestVerification(ctx, auth.RequestVerificationParams{
		Intent:          auth.IntentSignup,
		Phone:           rawPhone,
		Role:            auth.RoleCoach,
		Language:        auth.LanguageArabic,
		GoogleLinkToken: &linkToken,
	})
	if err != nil {
		t.Fatalf("RequestVerification: %v", err)
	}

	confirmRes, err := h.svc.ConfirmVerification(ctx, v.ID, h.sender.lastCode)
	if err != nil {
		t.Fatalf("ConfirmVerification: %v", err)
	}
	if confirmRes.Route != auth.RouteOnboarding {
		t.Fatalf("route = %q, want onboarding", confirmRes.Route)
	}
	if confirmRes.Account.GoogleSubject == nil || *confirmRes.Account.GoogleSubject != subject {
		t.Fatalf("account GoogleSubject = %v, want %q", confirmRes.Account.GoogleSubject, subject)
	}
	if confirmRes.Account.GoogleEmail == nil || *confirmRes.Account.GoogleEmail != email {
		t.Fatalf("account GoogleEmail = %v, want %q", confirmRes.Account.GoogleEmail, email)
	}
}

// TestRequestVerificationRejectsForgedGoogleLinkToken proves a client cannot
// link an account to an arbitrary Google identity by supplying its own
// google_link_token: only a token this service itself signed (via
// GoogleExchange, over a real verified identity) is accepted. This is the fix
// for the account-hijacking-via-unverified-identity-linking finding — the
// request must fail, and critically must not create any account.
func TestRequestVerificationRejectsForgedGoogleLinkToken(t *testing.T) {
	ctx := context.Background()
	h := newHarness()

	forged := "not-a-real-token"
	_, err := h.svc.RequestVerification(ctx, auth.RequestVerificationParams{
		Intent:          auth.IntentSignup,
		Phone:           rawPhone,
		Role:            auth.RoleCoach,
		Language:        auth.LanguageArabic,
		GoogleLinkToken: &forged,
	})
	if !errors.Is(err, auth.ErrGoogleFailed) {
		t.Fatalf("error = %v, want ErrGoogleFailed", err)
	}
	if h.sender.calls != 0 {
		t.Fatalf("sender calls = %d, want 0 (a forged link token must be rejected before any send)", h.sender.calls)
	}
	if len(h.verifs.byID) != 0 {
		t.Fatal("a forged link token must persist no challenge")
	}
}

// TestRequestVerificationRejectsGoogleLinkTokenFromDifferentSecret proves a
// token signed with a different secret (e.g. forged with a guessed or leaked
// key that isn't this service's actual secret) is rejected the same way.
func TestRequestVerificationRejectsGoogleLinkTokenFromDifferentSecret(t *testing.T) {
	ctx := context.Background()
	h := newHarness()

	// Stand up a second service instance with a *different* link secret, and use
	// its GoogleExchange to mint a token under that key — simulating an attacker
	// who doesn't know this service's actual secret.
	otherIdentity := newFakeIdentityVerifier()
	otherIdentity.identities["good-token"] = &auth.GoogleIdentity{Subject: "victim-subject", Email: "victim@example.com"}
	otherSvc := auth.NewService(
		&fakeClock{now: base}, &fakeSender{}, otherIdentity, newFakeAccounts(), newFakeVerifications(),
		&fakeSessions{}, &fakeAuditEvents{}, &fakeTx{}, []byte("a-completely-different-secret"),
	)
	exchangeRes, err := otherSvc.GoogleExchange(ctx, "good-token", nil)
	if err != nil {
		t.Fatalf("GoogleExchange (other): %v", err)
	}

	_, err = h.svc.RequestVerification(ctx, auth.RequestVerificationParams{
		Intent:          auth.IntentSignup,
		Phone:           rawPhone,
		Role:            auth.RoleCoach,
		Language:        auth.LanguageArabic,
		GoogleLinkToken: &exchangeRes.PendingLinkToken,
	})
	if !errors.Is(err, auth.ErrGoogleFailed) {
		t.Fatalf("error = %v, want ErrGoogleFailed", err)
	}
	if len(h.accounts.byPhone) != 0 {
		t.Fatal("a token signed under a different secret must not lead to account creation")
	}
}

// --- admin account list ---

// TestListAccountsReflectsMethodGoogleLinkedAndLockout proves the list derives
// google_linked/signup_method from GoogleSubject and lockout_active from the
// phone's latest Verification challenge.
func TestListAccountsReflectsMethodGoogleLinkedAndLockout(t *testing.T) {
	ctx := context.Background()
	h := newHarness()

	phoneAccountID := uuid.New()
	h.accounts.byPhone["+201000000001"] = &auth.Account{
		ID: phoneAccountID, Role: auth.RoleAthlete, Phone: "+201000000001",
		PreferredLanguage: auth.LanguageArabic, CreatedAt: base,
	}
	googleSubject := "g-sub-list"
	googleAccountID := uuid.New()
	h.accounts.byPhone["+201000000002"] = &auth.Account{
		ID: googleAccountID, Role: auth.RoleCoach, Phone: "+201000000002",
		GoogleSubject: &googleSubject, PreferredLanguage: auth.LanguageEnglish, CreatedAt: base.Add(time.Minute),
	}

	lockedVerifID := uuid.New()
	lockedUntil := base.Add(auth.LockoutDuration)
	h.verifs.byID[lockedVerifID] = &auth.Verification{
		ID: lockedVerifID, Phone: "+201000000001", SentAt: base, ExpiresAt: base.Add(auth.OTPTTL),
		LockedUntil: &lockedUntil, Status: auth.StatusLocked, FailedAttempts: auth.MaxFailedAttempts,
	}

	items, err := h.svc.ListAccounts(ctx, "", 50, 0)
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}

	byID := map[uuid.UUID]auth.AccountListItem{}
	for _, it := range items {
		byID[it.ID] = it
	}

	phoneItem := byID[phoneAccountID]
	if phoneItem.GoogleLinked || phoneItem.SignupMethod != auth.SignupMethodPhone {
		t.Fatalf("phone account: GoogleLinked=%v SignupMethod=%q, want false/phone", phoneItem.GoogleLinked, phoneItem.SignupMethod)
	}
	if !phoneItem.LockoutActive {
		t.Fatal("phone account should report lockout_active = true")
	}

	googleItem := byID[googleAccountID]
	if !googleItem.GoogleLinked || googleItem.SignupMethod != auth.SignupMethodGoogle {
		t.Fatalf("google account: GoogleLinked=%v SignupMethod=%q, want true/google", googleItem.GoogleLinked, googleItem.SignupMethod)
	}
	if googleItem.LockoutActive {
		t.Fatal("google account should report lockout_active = false (no challenge sent for its phone)")
	}
}

// TestListAccountsFiltersByPhone proves the phone filter narrows the list.
func TestListAccountsFiltersByPhone(t *testing.T) {
	ctx := context.Background()
	h := newHarness()

	h.accounts.byPhone["+201011111111"] = &auth.Account{ID: uuid.New(), Phone: "+201011111111", Role: auth.RoleCoach, CreatedAt: base}
	h.accounts.byPhone["+201022222222"] = &auth.Account{ID: uuid.New(), Phone: "+201022222222", Role: auth.RoleAthlete, CreatedAt: base}

	items, err := h.svc.ListAccounts(ctx, "1111", 50, 0)
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(items) != 1 || items[0].Phone != "+201011111111" {
		t.Fatalf("filtered items = %+v, want just +201011111111", items)
	}
}

// Lockout state must cost one lookup for the whole page, not one per account.
func TestListAccountsResolvesLockoutInOneLookup(t *testing.T) {
	ctx := context.Background()
	h := newHarness()

	for i := range 25 {
		phone := fmt.Sprintf("+2010%07d", i)
		h.accounts.byPhone[phone] = &auth.Account{ID: uuid.New(), Phone: phone, Role: auth.RoleAthlete, CreatedAt: base}
	}

	items, err := h.svc.ListAccounts(ctx, "", 50, 0)
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(items) != 25 {
		t.Fatalf("items = %d, want 25", len(items))
	}
	if h.verifs.lockedPhonesCalls != 1 {
		t.Fatalf("LockedPhones calls = %d, want 1 for the whole page", h.verifs.lockedPhonesCalls)
	}
}

// --- admin clears a lockout ---

// TestClearLockoutReenablesPhoneAndWritesOneAuditEvent drives a real
// 5-failed-attempts lockout (signin, so ClearLockout has an account to key
// off), proves the phone is genuinely stuck (a resend hits ErrLocked) before
// the clear, then proves the clear both records exactly one audit event and
// lets the very next resend on the same challenge succeed.
func TestClearLockoutReenablesPhoneAndWritesOneAuditEvent(t *testing.T) {
	ctx := context.Background()
	h := newHarness()

	const rawPhone2 = "01033333333"
	const normalizedPhone2 = "+201033333333"
	accountID := uuid.New()
	h.accounts.byPhone[normalizedPhone2] = &auth.Account{
		ID: accountID, Phone: normalizedPhone2, Role: auth.RoleAthlete, PreferredLanguage: auth.LanguageArabic, CreatedAt: base,
	}

	v, err := h.svc.RequestVerification(ctx, auth.RequestVerificationParams{Intent: auth.IntentSignin, Phone: rawPhone2})
	if err != nil {
		t.Fatalf("RequestVerification: %v", err)
	}
	wrong := wrongCodeFor(h.sender.lastCode)
	for attempt := 1; attempt < auth.MaxFailedAttempts; attempt++ {
		if _, err := h.svc.ConfirmVerification(ctx, v.ID, wrong); err == nil {
			t.Fatalf("attempt %d: expected an incorrect-code error", attempt)
		}
	}
	if _, err := h.svc.ConfirmVerification(ctx, v.ID, wrong); !errors.Is(err, auth.ErrLocked) {
		t.Fatalf("fifth attempt error = %v, want ErrLocked", err)
	}

	// Past the resend cooldown but still well inside the lock, so a resend is
	// blocked by the lock itself, not the cooldown.
	h.clock.now = base.Add(auth.ResendCooldown)
	if _, err := h.svc.ResendVerification(ctx, v.ID); !errors.Is(err, auth.ErrLocked) {
		t.Fatalf("resend before clear error = %v, want ErrLocked", err)
	}

	cleared, err := h.svc.ClearLockout(ctx, "ops-team", accountID)
	if err != nil {
		t.Fatalf("ClearLockout: %v", err)
	}
	if !cleared {
		t.Fatal("cleared = false, want true")
	}
	if len(h.audit.created) != 1 {
		t.Fatalf("audit events = %d, want 1", len(h.audit.created))
	}
	event := h.audit.created[0]
	if event.ActorAdminID != "ops-team" || event.Action != auth.AuditActionClearLockout || event.TargetAccountID != accountID {
		t.Fatalf("audit event = %+v, want actor=ops-team action=%q target=%v", event, auth.AuditActionClearLockout, accountID)
	}

	// The user can immediately resend on the very challenge that was locked —
	// proves the clear had a real effect, not merely that time passed.
	if _, err := h.svc.ResendVerification(ctx, v.ID); err != nil {
		t.Fatalf("resend after clear: %v", err)
	}
}

// TestClearLockoutNoOpWhenNotLocked proves clearing an account with no active
// lockout is a no-op: cleared=false and no audit event.
func TestClearLockoutNoOpWhenNotLocked(t *testing.T) {
	ctx := context.Background()
	h := newHarness()

	accountID := uuid.New()
	h.accounts.byPhone["+201044444444"] = &auth.Account{ID: accountID, Phone: "+201044444444", Role: auth.RoleCoach, CreatedAt: base}

	cleared, err := h.svc.ClearLockout(ctx, "ops-team", accountID)
	if err != nil {
		t.Fatalf("ClearLockout: %v", err)
	}
	if cleared {
		t.Fatal("cleared = true, want false (no active lockout)")
	}
	if len(h.audit.created) != 0 {
		t.Fatalf("audit events = %d, want 0", len(h.audit.created))
	}
}

// TestClearLockoutUnknownAccountNotFound proves an unknown account id surfaces
// ErrNotFound rather than a silent no-op, and emits no audit event.
func TestClearLockoutUnknownAccountNotFound(t *testing.T) {
	ctx := context.Background()
	h := newHarness()

	if _, err := h.svc.ClearLockout(ctx, "ops-team", uuid.New()); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if len(h.audit.created) != 0 {
		t.Fatal("no audit event for an unknown account")
	}
}

// --- admin analytics ---

// TestAnalyticsReflectsOnlyInRangeActivity seeds a mix of in-range and
// out-of-range signups (both methods) and verifications (verified/locked) and
// proves the three aggregates count only the in-range activity.
func TestAnalyticsReflectsOnlyInRangeActivity(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	since24h := base.Add(-24 * time.Hour)

	// In range: one phone signup, one Google signup.
	h.accounts.byPhone["+201055500001"] = &auth.Account{ID: uuid.New(), Phone: "+201055500001", Role: auth.RoleAthlete, CreatedAt: base.Add(-time.Hour)}
	googleSub := "g-analytics"
	h.accounts.byPhone["+201055500002"] = &auth.Account{ID: uuid.New(), Phone: "+201055500002", Role: auth.RoleCoach, GoogleSubject: &googleSub, CreatedAt: base.Add(-2 * time.Hour)}
	// Out of range: must not be counted.
	h.accounts.byPhone["+201055500003"] = &auth.Account{ID: uuid.New(), Phone: "+201055500003", Role: auth.RoleAthlete, CreatedAt: since24h.Add(-time.Hour)}

	// In range: one verified challenge with 2 prior failed attempts.
	h.verifs.byID[uuid.New()] = &auth.Verification{
		ID: uuid.New(), Phone: "+201055500001", SentAt: base.Add(-time.Hour), Status: auth.StatusVerified, FailedAttempts: 2,
	}
	// In range: one locked challenge (5 failed attempts, never verified).
	lockedUntil := base.Add(auth.LockoutDuration)
	h.verifs.byID[uuid.New()] = &auth.Verification{
		ID: uuid.New(), Phone: "+201055500002", SentAt: base.Add(-30 * time.Minute),
		Status: auth.StatusLocked, FailedAttempts: auth.MaxFailedAttempts, LockedUntil: &lockedUntil,
	}
	// Out of range: a locked challenge sent before the cutoff, must not count.
	h.verifs.byID[uuid.New()] = &auth.Verification{
		ID: uuid.New(), Phone: "+201055500003", SentAt: since24h.Add(-time.Hour), Status: auth.StatusLocked, FailedAttempts: auth.MaxFailedAttempts,
	}

	res, err := h.svc.Analytics(ctx, "24h")
	if err != nil {
		t.Fatalf("Analytics: %v", err)
	}

	if res.SignupsByMethod[auth.SignupMethodPhone] != 1 || res.SignupsByMethod[auth.SignupMethodGoogle] != 1 {
		t.Fatalf("SignupsByMethod = %+v, want phone=1 google=1", res.SignupsByMethod)
	}

	// failed = 2 (verified row) + 5 (locked row) = 7; verified = 1 -> 7/8.
	if want := 7.0 / 8.0; res.VerificationFailureRate != want {
		t.Fatalf("VerificationFailureRate = %v, want %v", res.VerificationFailureRate, want)
	}
	if res.LockoutCount != 1 {
		t.Fatalf("LockoutCount = %d, want 1 (only the in-range locked challenge)", res.LockoutCount)
	}
}

// TestAnalyticsRejectsInvalidRange proves an unsupported range value is
// rejected rather than silently defaulting.
func TestAnalyticsRejectsInvalidRange(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	if _, err := h.svc.Analytics(ctx, "bogus"); !errors.Is(err, auth.ErrInvalidRange) {
		t.Fatalf("error = %v, want ErrInvalidRange", err)
	}
}
