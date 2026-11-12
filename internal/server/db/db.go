// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package db wraps the Postgres pool, per-request user scoping and migrations.
package db

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"time"

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

// Listen runs LISTEN on a dedicated connection and calls onNotify per notification; it reconnects with backoff
// (1 s..30 s) and returns nil once ctx is done. ready (may be nil) is called after every successful LISTEN.
func (d *DB) Listen(ctx context.Context, channel string, ready func(), onNotify func(payload string)) error {
	if !identRe.MatchString(channel) {
		return fmt.Errorf("invalid channel name %q", channel)
	}
	listen := "LISTEN " + pgx.Identifier{channel}.Sanitize()
	backoff := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		err := d.listenOnce(ctx, listen, ready, onNotify)
		if ctx.Err() != nil {
			return nil
		}
		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		slog.Warn("listen: connection lost, retrying", "channel", channel, "err", err, "in", backoff)
		select {
		case <-ctx.Done():
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
	return nil
}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

func (d *DB) listenOnce(ctx context.Context, listen string, ready func(), onNotify func(string)) error {
	c, err := pgx.ConnectConfig(ctx, d.pool.Config().ConnConfig.Copy())
	if err != nil {
		return err
	}
	defer func() { _ = c.Close(context.WithoutCancel(ctx)) }()
	if _, err := c.Exec(ctx, listen); err != nil {
		return err
	}
	if ready != nil {
		ready()
	}
	for {
		n, err := c.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		onNotify(n.Payload)
	}
}
