// Package store holds the pgx pool wrapper and sqlc-generated queries.
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store wraps a pgx pool together with the generated query set.
type Store struct {
	*Queries
	Pool *pgxpool.Pool
}

// Connect opens a pgx pool, verifies connectivity, and returns a Store.
func Connect(ctx context.Context, url string, maxConns int32) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Store{Queries: New(pool), Pool: pool}, nil
}

// Ping checks database connectivity (used by /healthz).
func (s *Store) Ping(ctx context.Context) error {
	return s.Pool.Ping(ctx)
}

// Tx runs fn inside a transaction, committing on success and rolling back on
// error or panic.
func (s *Store) Tx(ctx context.Context, fn func(*Queries) error) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	if err := fn(s.Queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Close releases the pool.
func (s *Store) Close() {
	if s.Pool != nil {
		s.Pool.Close()
	}
}
