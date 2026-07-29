package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type txCtxKey struct{}

// TxManager runs functions inside a database transaction (intra-feature Unit
// of Work). Cross-feature atomicity: prefer tx-bound construction; this
// ambient path is the fallback and the intra-feature default.
type TxManager struct{ pool *pgxpool.Pool }

func NewTxManager(pool *pgxpool.Pool) *TxManager { return &TxManager{pool: pool} }

// InTx begins a transaction, stores it in ctx, runs fn, then commits (or rolls
// back on error/panic). Repos inside fn use Querier(ctx, pool) to pick up the
// tx.
func (m *TxManager) InTx(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: begin tx: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()
	if err = fn(context.WithValue(ctx, txCtxKey{}, tx)); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return fmt.Errorf("db: rollback after %w: %w", err, rbErr)
		}
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: commit: %w", err)
	}
	return nil
}

// Querier returns the ambient transaction if InTx is active, otherwise the
// pool.
func Querier(ctx context.Context, pool *pgxpool.Pool) DBTX {
	if tx, ok := ctx.Value(txCtxKey{}).(pgx.Tx); ok {
		return tx
	}
	return pool
}
