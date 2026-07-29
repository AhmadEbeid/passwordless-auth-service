//go:build integration

package db_test

import (
	"context"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/config"
	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/db"
)

func TestInTx_CommitAndRollback(t *testing.T) {
	ctx := context.Background()
	pg, err := tcpostgres.Run(
		ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("5432/tcp")),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = pg.Terminate(ctx) })

	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("dsn: %v", err)
	}
	pool, err := db.NewPool(ctx, config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, `CREATE TABLE t (id int primary key)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	mgr := db.NewTxManager(pool)

	// commit path
	if err := mgr.InTx(ctx, func(ctx context.Context) error {
		_, err := db.Querier(ctx, pool).Exec(ctx, `INSERT INTO t (id) VALUES (1)`)
		return err
	}); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	// rollback path
	_ = mgr.InTx(ctx, func(ctx context.Context) error {
		_, _ = db.Querier(ctx, pool).Exec(ctx, `INSERT INTO t (id) VALUES (2)`)
		return context.DeadlineExceeded // force rollback
	})

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM t`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("row count = %d, want 1 (commit kept, rollback discarded)", count)
	}
}
