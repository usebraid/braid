// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package derivetest is test support for the derive engine: it stores raw data as ingest would and inspects the derived tables.
package derivetest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/usebraid/braid/internal/server/db"
	"github.com/usebraid/braid/internal/server/derive"
	"github.com/usebraid/braid/internal/testdb"
)

// Env is a migrated test database with an application pool.
type Env struct {
	Owner, App string
	DB         *db.DB
}

// File is one file to store: Lines for an append file, JSON for a snapshot file.
type File struct {
	Rel   string
	Lines []string
	JSON  string
}

// New creates a fresh database.
func New(t testing.TB) *Env {
	t.Helper()
	o, a := testdb.New(t)
	return Wrap(t, o, a)
}

// Wrap uses an existing database.
func Wrap(t testing.TB, ownerURL, appURL string) *Env {
	t.Helper()
	d, err := db.Open(context.Background(), appURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	return &Env{Owner: ownerURL, App: appURL, DB: d}
}

func (e *Env) owner(t testing.TB, fn func(ctx context.Context, c *pgx.Conn)) {
	t.Helper()
	ctx := context.Background()
	c, err := pgx.Connect(ctx, e.Owner)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(ctx) }()
	fn(ctx, c)
}

func (e *Env) scan(t testing.TB, q string, args []any, dest ...any) {
	t.Helper()
	e.owner(t, func(ctx context.Context, c *pgx.Conn) {
		if err := c.QueryRow(ctx, q, args...).Scan(dest...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	})
}

// User inserts a user and returns its id.
func (e *Env) User(t testing.TB, sub string) string {
	t.Helper()
	var id string
	e.scan(t, `INSERT INTO users(oidc_issuer, oidc_sub) VALUES ('test', $1) RETURNING id::text`, []any{sub}, &id)
	return id
}

// Device inserts a device and returns its id.
func (e *Env) Device(t testing.TB, userID, name string) string {
	t.Helper()
	var id string
	e.scan(t, `INSERT INTO devices(user_id, name) VALUES ($1, $2) RETURNING id::text`, []any{userID, name}, &id)
	return id
}

func sum(b []byte) []byte { h := sha256.Sum256(b); return h[:] }

// Put stores files as ingest would, as owner (bypasses RLS).
func (e *Env) Put(t testing.TB, userID, deviceID string, files ...File) {
	t.Helper()
	e.owner(t, func(ctx context.Context, c *pgx.Conn) {
		for _, f := range files {
			var id, end int64
			err := c.QueryRow(ctx, `SELECT id, end_offset FROM source_files WHERE user_id = $1 AND rel_path = $2 ORDER BY id LIMIT 1`, userID, f.Rel).Scan(&id, &end)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				t.Fatal(err)
			}
			snapshot := f.JSON != ""
			if errors.Is(err, pgx.ErrNoRows) {
				head, kind := "snapshot", "snapshot"
				if !snapshot {
					head, kind = hex.EncodeToString(sum([]byte(f.Lines[0]))), "append"
				}
				if err := c.QueryRow(ctx, `INSERT INTO source_files(user_id, rel_path, head_hash, kind, first_seen, last_seen) VALUES ($1, $2, $3, $4, now(), now()) RETURNING id`,
					userID, f.Rel, head, kind).Scan(&id); err != nil {
					t.Fatal(err)
				}
			}
			if snapshot {
				h := sum([]byte(f.JSON))
				var same bool
				if err := c.QueryRow(ctx, `SELECT coalesce((SELECT content_hash = $2 FROM raw_snapshots WHERE source_file_id = $1 ORDER BY id DESC LIMIT 1), false)`, id, h).Scan(&same); err != nil {
					t.Fatal(err)
				}
				if !same {
					if _, err := c.Exec(ctx, `INSERT INTO raw_snapshots(user_id, source_file_id, content_hash, content, device_id) VALUES ($1, $2, $3, $4, $5)`,
						userID, id, h, []byte(f.JSON), deviceID); err != nil {
						t.Fatal(err)
					}
				}
				continue
			}
			for _, l := range f.Lines {
				if _, err := c.Exec(ctx, `INSERT INTO raw_lines(user_id, source_file_id, "offset", line_hash, line, device_id) VALUES ($1, $2, $3, $4, $5, $6)`,
					userID, id, end, sum([]byte(l)), []byte(l), deviceID); err != nil {
					t.Fatal(err)
				}
				end += int64(len(l)) + 1
			}
			if _, err := c.Exec(ctx, `UPDATE source_files SET end_offset = $2, last_seen = now() WHERE id = $1`, id, end); err != nil {
				t.Fatal(err)
			}
		}
	})
}

// Derive runs DeriveUser with the given batch size (0 = default) and returns the rows processed.
func (e *Env) Derive(t testing.TB, userID string, batch int) int {
	t.Helper()
	n, err := (&derive.Deriver{DB: e.DB, BatchLines: batch}).DeriveUser(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// Reset deletes the derived rows and cursors of the user.
func (e *Env) Reset(t testing.TB, userID string) {
	t.Helper()
	if err := (&derive.Deriver{DB: e.DB}).Reset(context.Background(), userID); err != nil {
		t.Fatal(err)
	}
}

// N runs an owner query returning one integer.
func (e *Env) N(t testing.TB, q string, args ...any) int {
	t.Helper()
	var n int
	e.scan(t, q, args, &n)
	return n
}

// Row runs an owner query and joins the first row's values with "|".
func (e *Env) Row(t testing.TB, q string, args ...any) string {
	t.Helper()
	var out string
	e.owner(t, func(ctx context.Context, c *pgx.Conn) {
		rows, err := c.Query(ctx, q, args...)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		defer rows.Close()
		if !rows.Next() {
			t.Fatalf("%s: no row (%v)", q, rows.Err())
		}
		vals, err := rows.Values()
		if err != nil {
			t.Fatal(err)
		}
		parts := make([]string, len(vals))
		for i, v := range vals {
			switch x := v.(type) {
			case nil:
				parts[i] = "<nil>"
			case float32, float64:
				parts[i] = fmt.Sprintf("%g", x)
			default:
				parts[i] = fmt.Sprint(x)
			}
		}
		out = strings.Join(parts, "|")
	})
	return out
}

// Dump is the canonical text of all derived tables of the user, with every key of repl replaced by its value.
func (e *Env) Dump(t testing.TB, userID string, repl map[string]string) string {
	t.Helper()
	var sb strings.Builder
	for _, tbl := range []string{"projects", "sessions", "session_devices", "agents", "tool_calls", "skill_phases", "usage", "failures", "search_docs", "derive_errors"} {
		var s string
		e.scan(t, `SELECT coalesce(string_agg(r, E'\n' ORDER BY r), '') FROM (SELECT (to_jsonb(x) - 'user_id' - 'source_file_id')::text AS r FROM `+tbl+` x WHERE user_id = $1) q`, []any{userID}, &s)
		sb.WriteString("== " + tbl + "\n" + s + "\n")
	}
	out := sb.String()
	for k, v := range repl {
		out = strings.ReplaceAll(out, k, v)
	}
	return out
}
