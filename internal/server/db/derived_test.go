// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package db_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/usebraid/braid/internal/testdb"
)

func TestEveryUserIDTableHasForcedRLS(t *testing.T) {
	f := setup(t)
	rows := strs(t, f.owner, `
	  SELECT c.table_name || ':' || (cl.relrowsecurity AND cl.relforcerowsecurity)::text || ':' ||
	         (EXISTS (SELECT 1 FROM pg_policies p WHERE p.tablename = c.table_name AND p.qual LIKE '%app_user()%'))::text
	    FROM information_schema.columns c
	    JOIN information_schema.tables t ON t.table_name = c.table_name AND t.table_schema = c.table_schema AND t.table_type = 'BASE TABLE'
	    JOIN pg_class cl ON cl.relname = c.table_name AND cl.relnamespace = 'public'::regnamespace
	   WHERE c.table_schema = 'public' AND c.column_name = 'user_id'
	     AND c.table_name <> 'device_codes'`) // device_codes has no app grants at all; it is reached only through definer functions
	if len(rows) < 18 {
		t.Fatalf("expected the SP1 and derived tables, got %v", rows)
	}
	for _, r := range rows {
		if !strings.HasSuffix(r, ":true:true") {
			t.Errorf("RLS not enforced with a user_id policy: %s", r)
		}
	}
}

func TestDerivedRowsAreIsolated(t *testing.T) {
	f := setup(t)
	testdb.Exec(t, f.owner, `INSERT INTO projects(user_id,path,name,first_seen_at,last_seen_at) VALUES ($1,'/w/a','a',now(),now())`, f.userA)
	if n := count(t, f.d, f.userA, "projects"); n != 1 {
		t.Fatalf("A sees %d", n)
	}
	if n := count(t, f.d, f.userB, "projects"); n != 0 {
		t.Fatalf("B sees %d", n)
	}
	if err := userExec(f, f.userB, `INSERT INTO projects(user_id,path,name,first_seen_at,last_seen_at) VALUES ($1,'/w/x','x',now(),now())`, f.userA); err == nil {
		t.Fatal("B inserted a row for A")
	}
	if err := userExec(f, f.userA, `DELETE FROM projects`); err != nil { // derived tables are a cache: app may delete
		t.Fatal(err)
	}
	if err := userExec(f, f.userA, `DELETE FROM raw_lines`); err == nil { // raw stays append-only
		t.Fatal("raw delete allowed")
	}
}

func TestWorkerFunctionsAreGuardedAndFindPendingWork(t *testing.T) {
	f := setup(t) // setup stores one raw line for A and none for B
	ids := func() []string {
		var out []string
		err := f.d.WithSystem(context.Background(), func(tx pgx.Tx) error {
			rows, err := tx.Query(context.Background(), `SELECT derive_pending_users()::text`)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var s string
				if err := rows.Scan(&s); err != nil {
					return err
				}
				out = append(out, s)
			}
			return rows.Err()
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if got := ids(); !slices.Equal(got, []string{f.userA}) {
		t.Fatalf("pending %v", got)
	}
	testdb.Exec(t, f.owner, `INSERT INTO derive_cursor(user_id,source_file_id,kind,last_line_id)
		SELECT user_id, id, 'main', (SELECT max(id) FROM raw_lines) FROM source_files`)
	if got := ids(); len(got) != 0 {
		t.Fatalf("pending after cursor %v", got)
	}
	for _, q := range []string{`SELECT derive_pending_users()`, `SELECT derive_all_users()`} {
		if err := userExec(f, f.userA, q); err == nil {
			t.Errorf("%s allowed in a user-scoped session", q)
		}
	}
}

func TestTrigramAndTimeZoneAsApp(t *testing.T) {
	f := setup(t)
	if err := userExec(f, f.userA, `SELECT similarity('handler', 'handlers'), 'internal/handler.go' ILIKE '%handl%'`); err != nil {
		t.Fatal(err)
	}
	if err := userExec(f, f.userA, `UPDATE users SET time_zone = 'Europe/Berlin'`); err != nil {
		t.Fatal(err)
	}
	if err := userExec(f, f.userA, `UPDATE users SET is_admin = true`); err == nil {
		t.Fatal("is_admin updatable")
	}
}

func TestAdminStatsFunctionRequiresAdmin(t *testing.T) {
	f := setup(t)
	admin := upsert(t, f.d, "sub-admin", true)
	call := func(user string) error {
		return f.d.WithUser(context.Background(), user, func(tx pgx.Tx) error {
			rows, err := tx.Query(context.Background(), `SELECT user_id FROM admin_stats()`)
			if err != nil {
				return err
			}
			rows.Close()
			return rows.Err()
		})
	}
	if err := call(f.userA); err == nil {
		t.Fatal("non-admin may call admin_stats")
	}
	if err := call(admin); err != nil {
		t.Fatal(err)
	}
}
