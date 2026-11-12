// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package db

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migrate applies all embedded migrations in name order, one transaction each.
func Migrate(ctx context.Context, ownerURL string) error { return MigrateTo(ctx, ownerURL, "") }

// MigrateTo applies migrations up to and including the file named last ("" = all).
func MigrateTo(ctx context.Context, ownerURL, last string) error {
	conn, err := pgx.Connect(ctx, ownerURL)
	if err != nil {
		return fmt.Errorf("migrate connect: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	entries, err := migrationFS.ReadDir("migrations") // sorted by filename
	if err != nil {
		return err
	}
	for _, e := range entries {
		sql, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		if err := apply(ctx, conn, e.Name(), string(sql)); err != nil {
			return fmt.Errorf("migration %s: %w", e.Name(), err)
		}
		if e.Name() == last {
			break
		}
	}
	return nil
}

func apply(ctx context.Context, conn *pgx.Conn, name, sql string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(4242)`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	var done bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name = $1)`, name).Scan(&done); err != nil {
		return err
	}
	if done {
		return tx.Commit(ctx)
	}
	// Simple protocol so a file may hold many statements.
	if _, err := tx.Exec(ctx, sql, pgx.QueryExecModeSimpleProtocol); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(name) VALUES ($1)`, name); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
