//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/auth"
	authpg "github.com/AhmadEbeid/passwordless-auth-service/internal/auth/postgres"
	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/config"
	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/db"
	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/observability"
)

func newRepos(t *testing.T) authpg.Repositories {
	t.Helper()
	ctx := context.Background()

	pg, err := tcpostgres.Run(
		ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		// Postgres binds the port during init and then restarts, so waiting on
		// the port alone races the restart. This waits for the readiness log
		// twice and then the port, per the module's own guidance.
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = pg.Terminate(ctx) })

	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("dsn: %v", err)
	}

	applyMigrations(t, dsn)

	pool, err := db.NewPool(ctx, config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	return authpg.New(pool)
}

// applyMigrations runs the shared goose migrations against the container,
// mirroring cmd/migrate.go but rooting the embedded-style FS at the repo dir
// resolved from this test file's location (robust to the test's cwd).
func applyMigrations(t *testing.T, dsn string) {
	t.Helper()

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")

	goose.SetBaseFS(os.DirFS(repoRoot))
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.Up(sqlDB, "migrations"); err != nil {
		t.Fatalf("goose up: %v", err)
	}
}

func TestAccountCreateFindAndUniqueness(t *testing.T) {
	ctx := context.Background()
	repos := newRepos(t)

	const phone = "+201012345678"
	account := &auth.Account{
		ID:                uuid.New(),
		Role:              auth.RoleCoach,
		Phone:             phone,
		PreferredLanguage: auth.LanguageArabic,
		CreatedAt:         time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := repos.Accounts.Create(ctx, account); err != nil {
		t.Fatalf("create account: %v", err)
	}

	got, err := repos.Accounts.FindByPhone(ctx, phone)
	if err != nil {
		t.Fatalf("find by phone: %v", err)
	}
	if got.ID != account.ID || got.Role != auth.RoleCoach || got.PreferredLanguage != auth.LanguageArabic {
		t.Fatalf("round-tripped account mismatch: %+v", got)
	}

	// Not-found maps to the domain sentinel.
	if _, err := repos.Accounts.FindByPhone(ctx, "+201099999999"); err != auth.ErrNotFound {
		t.Fatalf("find missing error = %v, want ErrNotFound", err)
	}

	// Duplicate phone violates the unique constraint → ErrAccountExists.
	dup := *account
	dup.ID = uuid.New()
	if err := repos.Accounts.Create(ctx, &dup); err != auth.ErrAccountExists {
		t.Fatalf("duplicate create error = %v, want ErrAccountExists", err)
	}
}

func TestVerificationRoundTrip(t *testing.T) {
	ctx := context.Background()
	repos := newRepos(t)

	now := time.Now().UTC().Truncate(time.Microsecond)
	v := &auth.Verification{
		ID:              uuid.New(),
		Phone:           "+201012345678",
		Intent:          auth.IntentSignup,
		CodeHash:        "$2a$10$abcdefghijklmnopqrstuv",
		PendingRole:     auth.RoleAthlete,
		PendingLanguage: auth.LanguageEnglish,
		SentAt:          now,
		ExpiresAt:       now.Add(auth.OTPTTL),
		SendsInWindow:   1,
		Status:          auth.StatusPending,
	}
	if err := repos.Verifications.Create(ctx, v); err != nil {
		t.Fatalf("create verification: %v", err)
	}

	got, err := repos.Verifications.Get(ctx, v.ID)
	if err != nil {
		t.Fatalf("get verification: %v", err)
	}
	if got.Intent != auth.IntentSignup || got.PendingRole != auth.RoleAthlete ||
		got.PendingLanguage != auth.LanguageEnglish || got.Status != auth.StatusPending {
		t.Fatalf("round-tripped verification mismatch: %+v", got)
	}
	if got.SendsInWindow != 1 || got.FailedAttempts != 0 || got.LockedUntil != nil {
		t.Fatalf("unexpected counters: %+v", got)
	}

	// Mutate + persist via Update, then re-read.
	got.RegisterFailure(now)
	got.Lock(now)
	if err := repos.Verifications.Update(ctx, got); err != nil {
		t.Fatalf("update verification: %v", err)
	}
	reread, err := repos.Verifications.Get(ctx, v.ID)
	if err != nil {
		t.Fatalf("re-get verification: %v", err)
	}
	if reread.Status != auth.StatusLocked || reread.LockedUntil == nil || reread.FailedAttempts != 1 {
		t.Fatalf("update not persisted: %+v", reread)
	}

	if _, err := repos.Verifications.Get(ctx, uuid.New()); err != auth.ErrNotFound {
		t.Fatalf("get missing error = %v, want ErrNotFound", err)
	}
}

func TestVerificationPendingGoogleRoundTrip(t *testing.T) {
	ctx := context.Background()
	repos := newRepos(t)

	subject := "google-subject-123"
	email := "athlete@example.com"
	now := time.Now().UTC().Truncate(time.Microsecond)
	v := &auth.Verification{
		ID:                   uuid.New(),
		Phone:                "+201012345678",
		Intent:               auth.IntentSignup,
		CodeHash:             "$2a$10$abcdefghijklmnopqrstuv",
		PendingRole:          auth.RoleAthlete,
		PendingLanguage:      auth.LanguageEnglish,
		PendingGoogleSubject: &subject,
		PendingGoogleEmail:   &email,
		SentAt:               now,
		ExpiresAt:            now.Add(auth.OTPTTL),
		Status:               auth.StatusPending,
	}
	if err := repos.Verifications.Create(ctx, v); err != nil {
		t.Fatalf("create verification: %v", err)
	}

	got, err := repos.Verifications.Get(ctx, v.ID)
	if err != nil {
		t.Fatalf("get verification: %v", err)
	}
	if got.PendingGoogleSubject == nil || *got.PendingGoogleSubject != subject {
		t.Fatalf("PendingGoogleSubject = %v, want %q", got.PendingGoogleSubject, subject)
	}
	if got.PendingGoogleEmail == nil || *got.PendingGoogleEmail != email {
		t.Fatalf("PendingGoogleEmail = %v, want %q", got.PendingGoogleEmail, email)
	}
}

func TestAccountFindByIDAndGoogleSubject(t *testing.T) {
	ctx := context.Background()
	repos := newRepos(t)

	subject := "google-subject-456"
	email := "coach@example.com"
	account := &auth.Account{
		ID:                uuid.New(),
		Role:              auth.RoleCoach,
		Phone:             "+201099988877",
		GoogleSubject:     &subject,
		GoogleEmail:       &email,
		PreferredLanguage: auth.LanguageArabic,
		CreatedAt:         time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := repos.Accounts.Create(ctx, account); err != nil {
		t.Fatalf("create account: %v", err)
	}

	byID, err := repos.Accounts.FindByID(ctx, account.ID)
	if err != nil {
		t.Fatalf("find by id: %v", err)
	}
	if byID.Phone != account.Phone {
		t.Fatalf("find by id mismatch: %+v", byID)
	}

	byGoogle, err := repos.Accounts.FindByGoogleSubject(ctx, subject)
	if err != nil {
		t.Fatalf("find by google subject: %v", err)
	}
	if byGoogle.ID != account.ID {
		t.Fatalf("find by google subject mismatch: %+v", byGoogle)
	}

	if _, err := repos.Accounts.FindByID(ctx, uuid.New()); err != auth.ErrNotFound {
		t.Fatalf("find by id missing error = %v, want ErrNotFound", err)
	}
	if _, err := repos.Accounts.FindByGoogleSubject(ctx, "no-such-subject"); err != auth.ErrNotFound {
		t.Fatalf("find by google subject missing error = %v, want ErrNotFound", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	repos := newRepos(t)

	account := &auth.Account{
		ID:                uuid.New(),
		Role:              auth.RoleCoach,
		Phone:             "+201055566677",
		PreferredLanguage: auth.LanguageArabic,
		CreatedAt:         time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := repos.Accounts.Create(ctx, account); err != nil {
		t.Fatalf("create account: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	session := &auth.Session{
		ID:           uuid.New(),
		AccountID:    account.ID,
		TokenHash:    "token-hash-abc",
		CreatedAt:    now,
		LastActiveAt: now,
		ExpiresAt:    now.Add(auth.SessionIdleWindow),
	}
	if err := repos.Sessions.Create(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	got, err := repos.Sessions.FindByTokenHash(ctx, "token-hash-abc")
	if err != nil {
		t.Fatalf("find by token hash: %v", err)
	}
	if got.AccountID != account.ID || got.RevokedAt != nil {
		t.Fatalf("round-tripped session mismatch: %+v", got)
	}

	extended := now.Add(time.Hour)
	got.LastActiveAt = extended
	got.ExpiresAt = extended.Add(auth.SessionIdleWindow)
	if err := repos.Sessions.Update(ctx, got); err != nil {
		t.Fatalf("update session: %v", err)
	}
	reread, err := repos.Sessions.FindByTokenHash(ctx, "token-hash-abc")
	if err != nil {
		t.Fatalf("re-find by token hash: %v", err)
	}
	if !reread.ExpiresAt.Equal(extended.Add(auth.SessionIdleWindow)) {
		t.Fatalf("extended ExpiresAt not persisted: %+v", reread)
	}

	revokedAt := extended.Add(time.Minute)
	if err := repos.Sessions.RevokeByTokenHash(ctx, "token-hash-abc", revokedAt); err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	revoked, err := repos.Sessions.FindByTokenHash(ctx, "token-hash-abc")
	if err != nil {
		t.Fatalf("find after revoke: %v", err)
	}
	if revoked.RevokedAt == nil || !revoked.RevokedAt.Equal(revokedAt) {
		t.Fatalf("revoked session RevokedAt = %v, want %v", revoked.RevokedAt, revokedAt)
	}

	// Revoking again (idempotent) must not overwrite the original RevokedAt.
	if err := repos.Sessions.RevokeByTokenHash(ctx, "token-hash-abc", revokedAt.Add(time.Hour)); err != nil {
		t.Fatalf("second revoke: %v", err)
	}
	stillRevoked, err := repos.Sessions.FindByTokenHash(ctx, "token-hash-abc")
	if err != nil {
		t.Fatalf("find after second revoke: %v", err)
	}
	if !stillRevoked.RevokedAt.Equal(revokedAt) {
		t.Fatalf("second revoke changed RevokedAt: %v, want unchanged %v", stillRevoked.RevokedAt, revokedAt)
	}

	if _, err := repos.Sessions.FindByTokenHash(ctx, "no-such-hash"); err != auth.ErrNotFound {
		t.Fatalf("find missing session error = %v, want ErrNotFound", err)
	}
}

func TestAccountListFiltersAndOrdersNewestFirst(t *testing.T) {
	ctx := context.Background()
	repos := newRepos(t)

	older := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	newer := time.Now().UTC().Truncate(time.Microsecond)

	for _, a := range []*auth.Account{
		{ID: uuid.New(), Role: auth.RoleCoach, Phone: "+201091111111", PreferredLanguage: auth.LanguageArabic, CreatedAt: older},
		{ID: uuid.New(), Role: auth.RoleAthlete, Phone: "+201092222222", PreferredLanguage: auth.LanguageArabic, CreatedAt: newer},
	} {
		if err := repos.Accounts.Create(ctx, a); err != nil {
			t.Fatalf("create account %+v: %v", a, err)
		}
	}

	all, err := repos.Accounts.List(ctx, "", 10, 0)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("len(all) = %d, want 2", len(all))
	}
	if all[0].Phone != "+201092222222" || all[1].Phone != "+201091111111" {
		t.Fatalf("list order = [%s, %s], want newest first", all[0].Phone, all[1].Phone)
	}

	filtered, err := repos.Accounts.List(ctx, "222222", 10, 0)
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if len(filtered) != 1 || filtered[0].Phone != "+201092222222" {
		t.Fatalf("filtered = %+v, want just +201092222222", filtered)
	}

	limited, err := repos.Accounts.List(ctx, "", 1, 0)
	if err != nil {
		t.Fatalf("list limited: %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("len(limited) = %d, want 1", len(limited))
	}
}

func TestAccountCountCreatedSinceSplitsByGoogleLinked(t *testing.T) {
	ctx := context.Background()
	repos := newRepos(t)

	now := time.Now().UTC().Truncate(time.Microsecond)
	since := now.Add(-time.Hour)
	subject := "google-subject-analytics"

	if err := repos.Accounts.Create(ctx, &auth.Account{
		ID: uuid.New(), Role: auth.RoleCoach, Phone: "+201093333333", PreferredLanguage: auth.LanguageArabic, CreatedAt: now,
	}); err != nil {
		t.Fatalf("create phone account: %v", err)
	}
	if err := repos.Accounts.Create(ctx, &auth.Account{
		ID: uuid.New(), Role: auth.RoleAthlete, Phone: "+201094444444", GoogleSubject: &subject,
		PreferredLanguage: auth.LanguageArabic, CreatedAt: now,
	}); err != nil {
		t.Fatalf("create google account: %v", err)
	}
	if err := repos.Accounts.Create(ctx, &auth.Account{
		ID: uuid.New(), Role: auth.RoleCoach, Phone: "+201095555555", PreferredLanguage: auth.LanguageArabic, CreatedAt: since.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("create out-of-range account: %v", err)
	}

	phoneOnly, googleLinked, err := repos.Accounts.CountCreatedSince(ctx, since)
	if err != nil {
		t.Fatalf("count created since: %v", err)
	}
	if phoneOnly != 1 || googleLinked != 1 {
		t.Fatalf("phoneOnly=%d googleLinked=%d, want 1/1 (the out-of-range account excluded)", phoneOnly, googleLinked)
	}
}

func TestVerificationSumFailedAttemptsAndCountByStatusSince(t *testing.T) {
	ctx := context.Background()
	repos := newRepos(t)

	now := time.Now().UTC().Truncate(time.Microsecond)
	since := now.Add(-time.Hour)
	lockedUntil := now.Add(auth.LockoutDuration)

	inRangeVerified := &auth.Verification{
		ID: uuid.New(), Phone: "+201096666666", Intent: auth.IntentSignup, CodeHash: "$2a$10$abcdefghijklmnopqrstuv",
		SentAt: now, ExpiresAt: now.Add(auth.OTPTTL), FailedAttempts: 2, Status: auth.StatusVerified,
	}
	inRangeLocked := &auth.Verification{
		ID: uuid.New(), Phone: "+201097777777", Intent: auth.IntentSignup, CodeHash: "$2a$10$abcdefghijklmnopqrstuv",
		SentAt: now, ExpiresAt: now.Add(auth.OTPTTL), FailedAttempts: auth.MaxFailedAttempts, LockedUntil: &lockedUntil, Status: auth.StatusLocked,
	}
	outOfRangeLocked := &auth.Verification{
		ID: uuid.New(), Phone: "+201098888888", Intent: auth.IntentSignup, CodeHash: "$2a$10$abcdefghijklmnopqrstuv",
		SentAt: since.Add(-time.Hour), ExpiresAt: since.Add(-time.Hour).Add(auth.OTPTTL), FailedAttempts: auth.MaxFailedAttempts, Status: auth.StatusLocked,
	}
	for _, v := range []*auth.Verification{inRangeVerified, inRangeLocked, outOfRangeLocked} {
		if err := repos.Verifications.Create(ctx, v); err != nil {
			t.Fatalf("create verification %+v: %v", v, err)
		}
	}

	failed, err := repos.Verifications.SumFailedAttemptsSince(ctx, since)
	if err != nil {
		t.Fatalf("sum failed attempts since: %v", err)
	}
	if want := 2 + auth.MaxFailedAttempts; failed != want {
		t.Fatalf("failed = %d, want %d (out-of-range row excluded)", failed, want)
	}

	verifiedCount, err := repos.Verifications.CountByStatusSince(ctx, auth.StatusVerified, since)
	if err != nil {
		t.Fatalf("count verified since: %v", err)
	}
	if verifiedCount != 1 {
		t.Fatalf("verifiedCount = %d, want 1", verifiedCount)
	}

	lockedCount, err := repos.Verifications.CountByStatusSince(ctx, auth.StatusLocked, since)
	if err != nil {
		t.Fatalf("count locked since: %v", err)
	}
	if lockedCount != 1 {
		t.Fatalf("lockedCount = %d, want 1 (the out-of-range locked row excluded)", lockedCount)
	}
}

func TestAuditEventCreate(t *testing.T) {
	ctx := context.Background()
	repos := newRepos(t)

	account := &auth.Account{
		ID:                uuid.New(),
		Role:              auth.RoleAthlete,
		Phone:             "+201044455566",
		PreferredLanguage: auth.LanguageArabic,
		CreatedAt:         time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := repos.Accounts.Create(ctx, account); err != nil {
		t.Fatalf("create account: %v", err)
	}

	event := &auth.AuditEvent{
		ID:              uuid.New(),
		ActorAdminID:    "ops-team",
		Action:          auth.AuditActionClearLockout,
		TargetAccountID: account.ID,
		CreatedAt:       time.Now().UTC().Truncate(time.Microsecond),
		Metadata:        map[string]any{"reason": "user requested"},
	}
	if err := repos.AuditEvents.Create(ctx, event); err != nil {
		t.Fatalf("create audit event: %v", err)
	}
	// Append-only: no read/update API exists. Success == the insert satisfied the
	// FK/NOT NULL constraints against a real account row.
}

// LockedPhones replaced a per-account lookup, so it needs to prove three things
// against real Postgres: the []string binds to ANY($1), DISTINCT ON picks the
// most recently sent row per phone rather than an arbitrary one, and the lock
// is evaluated at the passed instant.
func TestLockedPhonesResolvesLatestRowPerPhone(t *testing.T) {
	ctx := context.Background()
	repos := newRepos(t)

	now := time.Now().UTC().Truncate(time.Microsecond)
	locked := now.Add(30 * time.Minute)

	mk := func(phone string, sentAt time.Time, lockedUntil *time.Time) *auth.Verification {
		return &auth.Verification{
			ID: uuid.New(), Phone: phone, Intent: auth.IntentSignup,
			CodeHash: "hash", PendingRole: auth.RoleCoach, PendingLanguage: auth.LanguageArabic,
			SentAt: sentAt, ExpiresAt: sentAt.Add(10 * time.Minute),
			LockedUntil: lockedUntil, Status: auth.StatusPending,
		}
	}

	// The stale row is locked, the current one is not: a phone must read as
	// unlocked, which a query that ignored sent_at ordering would get wrong.
	for _, v := range []*auth.Verification{
		mk("+201090000001", now.Add(-time.Hour), &locked),
		mk("+201090000001", now, nil),
		mk("+201090000002", now.Add(-time.Hour), nil),
		mk("+201090000002", now, &locked),
		mk("+201090000003", now, nil),
	} {
		if err := repos.Verifications.Create(ctx, v); err != nil {
			t.Fatalf("create verification: %v", err)
		}
	}

	phones := []string{"+201090000001", "+201090000002", "+201090000003", "+201090000009"}
	got, err := repos.Verifications.LockedPhones(ctx, phones, now)
	if err != nil {
		t.Fatalf("LockedPhones: %v", err)
	}

	for phone, want := range map[string]bool{
		"+201090000001": false,
		"+201090000002": true,
		"+201090000003": false,
	} {
		if got[phone] != want {
			t.Errorf("LockedPhones[%s] = %v, want %v", phone, got[phone], want)
		}
	}
	// A phone with no challenge is absent, not false-by-accident.
	if _, ok := got["+201090000009"]; ok {
		t.Errorf("phone with no challenge should be absent from the result, got %v", got["+201090000009"])
	}

	// Past the lock's expiry every phone reads unlocked, proving now is what
	// decides rather than the stored status column.
	after, err := repos.Verifications.LockedPhones(ctx, phones, locked.Add(time.Minute))
	if err != nil {
		t.Fatalf("LockedPhones after expiry: %v", err)
	}
	for phone, v := range after {
		if v {
			t.Errorf("LockedPhones[%s] = true after the lock expired", phone)
		}
	}

	// Empty input must not reach the database with an empty array.
	empty, err := repos.Verifications.LockedPhones(ctx, nil, now)
	if err != nil || len(empty) != 0 {
		t.Fatalf("LockedPhones(nil) = %v, %v; want empty map, nil", empty, err)
	}
}

// The admin list must stay flat in the number of statements it issues: one for
// the page of accounts, one for their lockout state. Without the batch it grew
// with the row count, which is the regression this guards.
func TestAdminAccountListDoesNotQueryPerRow(t *testing.T) {
	ctx := context.Background()
	repos := newRepos(t)

	now := time.Now().UTC().Truncate(time.Microsecond)
	for i := range 25 {
		a := &auth.Account{
			ID: uuid.New(), Role: auth.RoleAthlete,
			Phone:             fmt.Sprintf("+2010%07d", i),
			PreferredLanguage: auth.LanguageArabic, CreatedAt: now,
		}
		if err := repos.Accounts.Create(ctx, a); err != nil {
			t.Fatalf("create account: %v", err)
		}
	}

	svc := auth.NewService(
		fixedClock{now}, nil, nil,
		repos.Accounts, repos.Verifications, repos.Sessions, repos.AuditEvents,
		noopTx{}, []byte("test-secret"),
	)

	counted := observability.ContextWithQueryCount(ctx)
	items, err := svc.ListAccounts(counted, "", 50, 0)
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(items) != 25 {
		t.Fatalf("items = %d, want 25", len(items))
	}
	if n := observability.QueryCount(counted); n != 2 {
		t.Fatalf("ListAccounts issued %d statements for 25 accounts, want 2", n)
	}
}

// LIKE metacharacters in the filter must match literally, not as wildcards.
func TestAccountListEscapesLikeWildcards(t *testing.T) {
	ctx := context.Background()
	repos := newRepos(t)

	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, phone := range []string{"+201090000001", "+201090000002"} {
		a := &auth.Account{
			ID: uuid.New(), Role: auth.RoleCoach, Phone: phone,
			PreferredLanguage: auth.LanguageArabic, CreatedAt: now,
		}
		if err := repos.Accounts.Create(ctx, a); err != nil {
			t.Fatalf("create account: %v", err)
		}
	}

	for _, filter := range []string{"%", "_"} {
		got, err := repos.Accounts.List(ctx, filter, 10, 0)
		if err != nil {
			t.Fatalf("list %q: %v", filter, err)
		}
		if len(got) != 0 {
			t.Errorf("filter %q matched %d accounts; wildcards must be literal", filter, len(got))
		}
	}
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type noopTx struct{}

func (noopTx) InTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }
