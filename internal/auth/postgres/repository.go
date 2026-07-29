// Package postgres holds the auth feature's Postgres adapters. It is one of
// the few packages permitted to import pgx, enforced by depguard; it maps
// between domain types and rows and translates pgx errors into the auth
// package's sentinel errors, so pgx never leaks past this boundary.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/auth"
	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/db"
)

const uniqueViolation = "23505"

// likeEscaper neutralises LIKE metacharacters in a user-supplied filter, so a
// search for "%" matches that literal character instead of every row.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func escapeLike(s string) string { return likeEscaper.Replace(s) }

// Repositories bundles the auth repository adapters over one pool. Each query
// runs through db.Querier so it picks up an ambient Unit-of-Work tx when one
// is active (uow.go).
type Repositories struct {
	Accounts      *AccountRepository
	Verifications *VerificationRepository
	Sessions      *SessionRepository
	AuditEvents   *AuditEventRepository
}

// New builds the auth repositories from a pool.
func New(pool *pgxpool.Pool) Repositories {
	return Repositories{
		Accounts:      &AccountRepository{pool: pool},
		Verifications: &VerificationRepository{pool: pool},
		Sessions:      &SessionRepository{pool: pool},
		AuditEvents:   &AuditEventRepository{pool: pool},
	}
}

// AccountRepository persists verified accounts.
type AccountRepository struct{ pool *pgxpool.Pool }

func (r *AccountRepository) FindByPhone(ctx context.Context, phone string) (*auth.Account, error) {
	row := db.Querier(ctx, r.pool).QueryRow(ctx, qAccountFindByPhone, phone)
	return scanAccount(row, "find account by phone")
}

func (r *AccountRepository) FindByID(ctx context.Context, id uuid.UUID) (*auth.Account, error) {
	row := db.Querier(ctx, r.pool).QueryRow(ctx, qAccountFindByID, id)
	return scanAccount(row, "find account by id")
}

func (r *AccountRepository) FindByGoogleSubject(ctx context.Context, subject string) (*auth.Account, error) {
	row := db.Querier(ctx, r.pool).QueryRow(ctx, qAccountFindByGoogleSubject, subject)
	return scanAccount(row, "find account by google subject")
}

// List returns accounts whose phone contains phoneFilter, newest-created
// first. An empty filter matches every account, the pattern collapsing to
// '%%'.
func (r *AccountRepository) List(ctx context.Context, phoneFilter string, limit, offset int) ([]auth.Account, error) {
	rows, err := db.Querier(ctx, r.pool).Query(ctx, qAccountList, escapeLike(phoneFilter), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("auth: list accounts: %w", err)
	}
	defer rows.Close()

	accounts := make([]auth.Account, 0, limit)
	for rows.Next() {
		a, err := scanAccount(rows, "list accounts")
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auth: list accounts: %w", err)
	}
	return accounts, nil
}

// CountCreatedSince splits accounts created at or after since into phone-only
// vs Google-linked counts, for the admin analytics signup breakdown.
func (r *AccountRepository) CountCreatedSince(ctx context.Context, since time.Time) (phoneOnly, googleLinked int, err error) {
	if err := db.Querier(ctx, r.pool).QueryRow(ctx, qAccountCountCreatedSince, since).Scan(&phoneOnly, &googleLinked); err != nil {
		return 0, 0, fmt.Errorf("auth: count accounts created since: %w", err)
	}
	return phoneOnly, googleLinked, nil
}

func scanAccount(row pgx.Row, op string) (*auth.Account, error) {
	var (
		a    auth.Account
		role string
		lang string
	)
	if err := row.Scan(&a.ID, &role, &a.Phone, &a.GoogleSubject, &a.GoogleEmail, &lang, &a.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, auth.ErrNotFound
		}
		return nil, fmt.Errorf("auth: %s: %w", op, err)
	}
	a.Role = auth.Role(role)
	a.PreferredLanguage = auth.Language(lang)
	return &a, nil
}

func (r *AccountRepository) Create(ctx context.Context, a *auth.Account) error {
	_, err := db.Querier(ctx, r.pool).Exec(ctx, qAccountCreate,
		a.ID, string(a.Role), a.Phone, a.GoogleSubject, a.GoogleEmail, string(a.PreferredLanguage), a.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return auth.ErrAccountExists
		}
		return fmt.Errorf("auth: create account: %w", err)
	}
	return nil
}

// VerificationRepository persists OTP challenges.
type VerificationRepository struct{ pool *pgxpool.Pool }

func (r *VerificationRepository) Create(ctx context.Context, v *auth.Verification) error {
	_, err := db.Querier(ctx, r.pool).Exec(ctx, qVerificationCreate,
		v.ID, v.Phone, string(v.Intent), v.CodeHash,
		nullableEnum(string(v.PendingRole)), nullableEnum(string(v.PendingLanguage)),
		v.PendingGoogleSubject, v.PendingGoogleEmail,
		v.SentAt, v.ExpiresAt, v.FailedAttempts, v.SendsInWindow, v.LockedUntil, string(v.Status))
	if err != nil {
		return fmt.Errorf("auth: create verification: %w", err)
	}
	return nil
}

// scanVerification maps a single verification row into a domain Verification,
// translating pgx.ErrNoRows into auth.ErrNotFound. Callers wrap other errors.
func scanVerification(row pgx.Row) (*auth.Verification, error) {
	var (
		v           auth.Verification
		intent      string
		status      string
		pendingRole *string
		pendingLang *string
	)
	if err := row.Scan(&v.ID, &v.Phone, &intent, &v.CodeHash, &pendingRole, &pendingLang,
		&v.PendingGoogleSubject, &v.PendingGoogleEmail,
		&v.SentAt, &v.ExpiresAt, &v.FailedAttempts, &v.SendsInWindow, &v.LockedUntil, &status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, auth.ErrNotFound
		}
		return nil, err
	}
	v.Intent = auth.Intent(intent)
	v.Status = auth.Status(status)
	if pendingRole != nil {
		v.PendingRole = auth.Role(*pendingRole)
	}
	if pendingLang != nil {
		v.PendingLanguage = auth.Language(*pendingLang)
	}
	return &v, nil
}

func (r *VerificationRepository) Get(ctx context.Context, id uuid.UUID) (*auth.Verification, error) {
	v, err := scanVerification(db.Querier(ctx, r.pool).QueryRow(ctx, qVerificationGet, id))
	if err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			return nil, auth.ErrNotFound
		}
		return nil, fmt.Errorf("auth: get verification: %w", err)
	}
	return v, nil
}

func (r *VerificationRepository) FindLatestByPhone(ctx context.Context, phone string) (*auth.Verification, error) {
	v, err := scanVerification(db.Querier(ctx, r.pool).QueryRow(ctx, qVerificationFindLatestByPhone, phone))
	if err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			return nil, auth.ErrNotFound
		}
		return nil, fmt.Errorf("auth: find latest verification by phone: %w", err)
	}
	return v, nil
}

// LockedPhones resolves each phone's most recently sent challenge and reports
// whether it is locked at now — the batched form of FindLatestByPhone +
// IsLocked, so the admin account list costs one query instead of one per row.
// Phones with no challenge on record are simply absent from the result.
func (r *VerificationRepository) LockedPhones(ctx context.Context, phones []string, now time.Time) (map[string]bool, error) {
	locked := make(map[string]bool, len(phones))
	if len(phones) == 0 {
		return locked, nil
	}

	rows, err := db.Querier(ctx, r.pool).Query(ctx, qVerificationLockedPhones, phones)
	if err != nil {
		return nil, fmt.Errorf("auth: locked phones: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			phone       string
			lockedUntil *time.Time
		)
		if err := rows.Scan(&phone, &lockedUntil); err != nil {
			return nil, fmt.Errorf("auth: locked phones: %w", err)
		}
		locked[phone] = lockedUntil != nil && now.Before(*lockedUntil)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auth: locked phones: %w", err)
	}
	return locked, nil
}

func (r *VerificationRepository) CountSendsSince(ctx context.Context, phone string, since time.Time) (int, error) {
	var n int
	if err := db.Querier(ctx, r.pool).QueryRow(ctx, qVerificationCountSendsSince, phone, since).Scan(&n); err != nil {
		return 0, fmt.Errorf("auth: count sends since: %w", err)
	}
	return n, nil
}

// SumFailedAttemptsSince sums failed_attempts across challenges sent at or
// after since, for admin analytics' verification_failure_rate.
func (r *VerificationRepository) SumFailedAttemptsSince(ctx context.Context, since time.Time) (int, error) {
	var n int
	if err := db.Querier(ctx, r.pool).QueryRow(ctx, qVerificationSumFailedAttemptsSince, since).Scan(&n); err != nil {
		return 0, fmt.Errorf("auth: sum failed attempts since: %w", err)
	}
	return n, nil
}

// CountByStatusSince counts challenges with the given status sent at or after
// since, for admin analytics.
func (r *VerificationRepository) CountByStatusSince(ctx context.Context, status auth.Status, since time.Time) (int, error) {
	var n int
	if err := db.Querier(ctx, r.pool).QueryRow(ctx, qVerificationCountByStatusSince, string(status), since).Scan(&n); err != nil {
		return 0, fmt.Errorf("auth: count by status since: %w", err)
	}
	return n, nil
}

func (r *VerificationRepository) Update(ctx context.Context, v *auth.Verification) error {
	tag, err := db.Querier(ctx, r.pool).Exec(ctx, qVerificationUpdate,
		v.ID, v.CodeHash, v.SentAt, v.ExpiresAt, v.FailedAttempts,
		v.SendsInWindow, v.LockedUntil, string(v.Status))
	if err != nil {
		return fmt.Errorf("auth: update verification: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return auth.ErrNotFound
	}
	return nil
}

// SessionRepository persists issued sessions.
type SessionRepository struct{ pool *pgxpool.Pool }

func (r *SessionRepository) Create(ctx context.Context, s *auth.Session) error {
	_, err := db.Querier(ctx, r.pool).Exec(ctx, qSessionCreate,
		s.ID, s.AccountID, s.TokenHash, s.DeviceLabel, s.CreatedAt, s.LastActiveAt, s.ExpiresAt, s.RevokedAt)
	if err != nil {
		return fmt.Errorf("auth: create session: %w", err)
	}
	return nil
}

func (r *SessionRepository) FindByTokenHash(ctx context.Context, tokenHash string) (*auth.Session, error) {
	var s auth.Session
	err := db.Querier(ctx, r.pool).QueryRow(ctx, qSessionFindByTokenHash, tokenHash).Scan(
		&s.ID, &s.AccountID, &s.TokenHash, &s.DeviceLabel, &s.CreatedAt, &s.LastActiveAt, &s.ExpiresAt, &s.RevokedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, auth.ErrNotFound
		}
		return nil, fmt.Errorf("auth: find session by token hash: %w", err)
	}
	return &s, nil
}

func (r *SessionRepository) Update(ctx context.Context, s *auth.Session) error {
	tag, err := db.Querier(ctx, r.pool).Exec(ctx, qSessionUpdate, s.ID, s.LastActiveAt, s.ExpiresAt)
	if err != nil {
		return fmt.Errorf("auth: update session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return auth.ErrNotFound
	}
	return nil
}

func (r *SessionRepository) RevokeByTokenHash(ctx context.Context, tokenHash string, revokedAt time.Time) error {
	if _, err := db.Querier(ctx, r.pool).Exec(ctx, qSessionRevokeByTokenHash, tokenHash, revokedAt); err != nil {
		return fmt.Errorf("auth: revoke session: %w", err)
	}
	return nil
}

// AuditEventRepository persists admin audit events. Append-only.
type AuditEventRepository struct{ pool *pgxpool.Pool }

func (r *AuditEventRepository) Create(ctx context.Context, e *auth.AuditEvent) error {
	var metadata []byte
	if e.Metadata != nil {
		var err error
		metadata, err = json.Marshal(e.Metadata)
		if err != nil {
			return fmt.Errorf("auth: marshal audit event metadata: %w", err)
		}
	}
	_, err := db.Querier(ctx, r.pool).Exec(ctx, qAuditEventCreate,
		e.ID, e.ActorAdminID, e.Action, e.TargetAccountID, e.CreatedAt, metadata)
	if err != nil {
		return fmt.Errorf("auth: create audit event: %w", err)
	}
	return nil
}

// nullableEnum maps an empty enum string to a SQL NULL so optional columns
// stay null rather than storing "".
func nullableEnum(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
