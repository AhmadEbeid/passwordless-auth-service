package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/db"
)

func TestQuerier_NoAmbientTx_ReturnsPool(t *testing.T) {
	// With no InTx active, Querier returns the pool it was given (typed
	// *pgxpool.Pool).
	var pool *pgxpool.Pool // nil is fine; we only assert the dynamic type.
	got := db.Querier(context.Background(), pool)
	if _, ok := got.(*pgxpool.Pool); !ok {
		t.Fatalf("expected *pgxpool.Pool passthrough, got %T", got)
	}
}
