// Package postgres owns the connection pool, the transaction runner, and the
// small set of pgx error predicates the domain packages need.
//
// It knows nothing about pledges, campaigns or films.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Pool = pgxpool.Pool

func Connect(ctx context.Context, dsn string, maxConns int32) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

func MustConnect(ctx context.Context, dsn string, maxConns int32) *Pool {
	pool, err := Connect(ctx, dsn, maxConns)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	return pool
}

// TxRunner runs a function inside a transaction, committing on nil and rolling
// back on error or panic.
//
// Services take this as a dependency rather than a *Pool so that "what is inside
// the transaction" is a visible, testable decision instead of an accident of
// which connection a repo happened to grab.
type TxRunner struct{ pool *Pool }

func NewTxRunner(pool *Pool) TxRunner { return TxRunner{pool: pool} }

func (t TxRunner) Do(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			return errors.Join(err, fmt.Errorf("rollback: %w", rbErr))
		}
		return err
	}
	// Commit is where DEFERRABLE constraint triggers fire - notably the ledger
	// balance assertion. A commit error here is a real domain failure, not
	// plumbing, so it is returned unchanged for the caller to classify.
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// IsUnique reports whether err is a unique-constraint violation (SQLSTATE 23505).
//
// This is load-bearing for idempotency: "already recorded" is signalled by the
// database refusing a duplicate, and callers turn it into a no-op rather than
// an error. See docs/07 §4.
func IsUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// ConstraintName returns the violated constraint's name, or "". Use it when a
// table has several unique constraints and they mean different things.
func ConstraintName(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.ConstraintName
	}
	return ""
}

// IsCheckViolation reports a CHECK constraint failure (23514) - e.g. the tier
// oversell guard or chk_payout_math.
func IsCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514"
}

// IsForeignKey reports a foreign-key violation (23503).
func IsForeignKey(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

// IsNoRows is a convenience wrapper so domain packages need not import pgx.
func IsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
