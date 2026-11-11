// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package db wraps the Postgres pool, per-request user scoping and migrations.
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB is the application pool; it connects as braid_app (no BYPASSRLS).
type DB struct{ pool *pgxpool.Pool }

// Open connects as braid_app.
func Open(ctx context.Context, appURL string) (*DB, error) {
	p, err := pgxpool.New(ctx, appURL)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	return &DB{pool: p}, nil
}

// Close closes the pool.
func (d *DB) Close() { d.pool.Close() }

// Ping checks connectivity.
func (d *DB) Ping(ctx context.Context) error { return d.pool.Ping(ctx) }

// WithUser runs fn in a transaction scoped (via RLS) to userID.
func (d *DB) WithUser(ctx context.Context, userID string, fn func(pgx.Tx) error) error {
	return d.run(ctx, func(tx pgx.Tx) error {
		// is_local=true: the setting dies with the transaction, so pooled connections never leak identity.
		if _, err := tx.Exec(ctx, `SELECT set_config('app.user_id', $1, true)`, userID); err != nil {
			return err
		}
		return fn(tx)
	})
}

// WithSystem runs fn in a transaction with no user; only definer functions return data.
func (d *DB) WithSystem(ctx context.Context, fn func(pgx.Tx) error) error {
	return d.run(ctx, fn)
}

func (d *DB) run(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
