// Package db owns the Postgres connection pool and the Unit of Work. It is the
// only package (besides feature postgres/ adapters and cmd/) permitted to
// import pgx, enforced by depguard.
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/config"
	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/observability"
)

// DBTX is the query surface shared by *pgxpool.Pool and pgx.Tx; sqlc-generated
// Queries accept this interface, so the same repo code runs on a pool or a tx.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// queryCountTracer tallies every statement against the counter on the calling
// context, which is what makes an N+1 access pattern show up as a number rather
// than as unexplained latency. It deliberately records nothing else — no SQL,
// no arguments — so it cannot leak a phone number or a token hash into a log.
type queryCountTracer struct{}

func (queryCountTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	observability.CountQuery(ctx)
	return ctx
}

func (queryCountTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// NewPool opens a pgx pool from cfg and verifies connectivity with a ping.
func NewPool(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("db: parse config: %w", err)
	}
	poolCfg.ConnConfig.Tracer = queryCountTracer{}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("db: open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return pool, nil
}
