// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/usebraid/braid/internal/testdb"
)

func userExec(f fixture, user, q string, args ...any) error {
	return f.d.WithUser(context.Background(), user, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), q, args...)
		return err
	})
}

// Attack tests: each statement must fail for braid_app.
func TestAppCannotEscalate(t *testing.T) {
	f := setup(t)
	const devA = `'00000000-0000-0000-0000-00000000000a'`
	cases := map[string]string{ //nolint:gosec // SQL attack strings, not credentials
		"own is_admin":              `UPDATE users SET is_admin = true`,
		"own device to other user":  `UPDATE devices SET user_id = gen_random_uuid()`,
		"call internal admin guard": `SELECT require_admin()`,
		"read schema_migrations":    `SELECT * FROM schema_migrations`,
		"forge token":               `INSERT INTO device_tokens(device_id,user_id,hash,scopes) VALUES (` + devA + `, '` + f.userA + `', '\x77', '{}')`,
		"insert user":               `INSERT INTO users(oidc_issuer,oidc_sub) VALUES ('i','s')`,
		"bypass via role":           `SET ROLE braid_owner`,
		"self-promote via upsert":   `SELECT upsert_oidc_user('i', 'sub-a', 'x', 'x', true) FROM users`,
		"forge audit for other":     `SELECT write_audit(gen_random_uuid(), gen_random_uuid(), NULL, 'x', NULL, NULL, NULL)`,
		"disable rls":               `ALTER TABLE raw_lines DISABLE ROW LEVEL SECURITY`,
	}
	for name, q := range cases {
		if err := userExec(f, f.userA, q); err == nil {
			t.Errorf("%s: statement succeeded", name)
		}
	}
}

// A user may not attach their own rows to another user's device (composite FK).
func TestRawRowsCannotUseForeignDevice(t *testing.T) {
	f := setup(t)
	testdb.Exec(t, f.owner, `INSERT INTO source_files(user_id,rel_path,head_hash,kind,first_seen,last_seen) VALUES ($1,'p/b.jsonl','h','append',now(),now())`, f.userB)
	err := userExec(f, f.userB, `INSERT INTO raw_lines(user_id,source_file_id,"offset",line_hash,line,device_id)
		SELECT $1,id,0,'\x02','\x7b7d','00000000-0000-0000-0000-00000000000a' FROM source_files WHERE user_id=$1`, f.userB)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("want foreign_key_violation, got %v", err)
	}
	err = userExec(f, f.userB, `INSERT INTO source_files(user_id,scope_device_id,rel_path,head_hash,kind,first_seen,last_seen) VALUES ($1,'00000000-0000-0000-0000-00000000000a','x','h','append',now(),now())`, f.userB)
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("scope_device_id: want foreign_key_violation, got %v", err)
	}
}

func TestUnsetUserWritesNothing(t *testing.T) {
	f := setup(t)
	err := f.d.WithSystem(context.Background(), func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO web_sessions(id_hash,user_id,expires_at) VALUES ('\x05',$1,now()+interval '1 day')`, f.userA)
		return err
	})
	if err == nil {
		t.Fatal("insert without app.user_id succeeded")
	}
}

func TestAuditIsolationAndDisabledApprover(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	_ = f.d.WithSystem(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT write_audit($1,$1,NULL,'login',NULL,'ua',NULL)`, f.userA)
		return err
	})
	if n := count(t, f.d, f.userA, "audit_log"); n != 1 {
		t.Errorf("A sees %d audit rows, want 1", n)
	}
	if n := count(t, f.d, f.userB, "audit_log"); n != 0 {
		t.Errorf("B sees %d audit rows, want 0", n)
	}
	if err := f.d.WithSystem(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT write_audit(NULL,NULL,NULL,'bogus',NULL,NULL,NULL)`)
		return err
	}); err == nil {
		t.Error("unknown audit action accepted")
	}
	// disabled approver gets nothing; a disabled approver after approval gets access_denied on poll
	admin := upsert(t, f.d, "adm", true)
	_ = f.d.WithSystem(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT device_code_create('\x40','\x41','{}',NULL)`)
		return err
	})
	var dev *string
	_ = f.d.WithUser(ctx, f.userB, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT device_code_decide('\x41', true, ' dev ')`).Scan(&dev)
	})
	if dev == nil {
		t.Fatal("approve failed")
	}
	if err := f.d.WithUser(ctx, admin, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT admin_set_disabled($1,true)`, f.userB)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var status string
	_ = f.d.WithSystem(ctx, func(tx pgx.Tx) error {
		var d *string
		var iv int
		return tx.QueryRow(ctx, `SELECT * FROM device_code_poll('\x40','\x42')`).Scan(&status, &d, &iv)
	})
	if status != "access_denied" {
		t.Errorf("poll after disable: %s", status)
	}
}

// B must get a foreign_key_violation, never a unique_violation, for tuples that exist for A.
func TestNoUniqueKeyOracle(t *testing.T) {
	f := setup(t)
	var fileID int64
	ownerRead(t, f.owner, `SELECT id FROM source_files`, &fileID) // B cannot see it via RLS; use A's guessable id
	err := userExec(f, f.userB, `INSERT INTO raw_lines(user_id,source_file_id,"offset",line_hash,line,device_id) VALUES ($1,$2,0,'\x01','\x7b7d','00000000-0000-0000-0000-00000000000a')`, f.userB, fileID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("want 23503, got %v", err)
	}
	// source_files(device, path, head_hash) of A must not collide for B either
	err = userExec(f, f.userB, `INSERT INTO source_files(user_id,scope_device_id,rel_path,head_hash,kind,first_seen,last_seen) VALUES ($1,'00000000-0000-0000-0000-00000000000a','p/a.jsonl','h','append',now(),now())`, f.userB)
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("source_files: want 23503, got %v", err)
	}
}

func TestAuthFunctionsPositive(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	testdb.Exec(t, f.owner, `INSERT INTO web_sessions(id_hash,user_id,created_at,last_seen_at,expires_at) VALUES ('\x51',$1,now()-interval '29 days',now()-interval '29 days',now()+interval '6 days')`, f.userA)
	testdb.Exec(t, f.owner, `INSERT INTO device_tokens(device_id,user_id,hash,scopes,created_at) VALUES ('00000000-0000-0000-0000-00000000000a',$1,'\x52','{ingest:write}',now()-interval '5 days')`, f.userA)
	var uid string
	var admin bool
	if err := f.d.WithSystem(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT * FROM auth_session('\x51')`).Scan(&uid, &admin)
	}); err != nil {
		t.Fatal(err)
	}
	if uid != f.userA || admin {
		t.Fatalf("auth_session: %s %v", uid, admin)
	}
	var created, expires time.Time
	var last time.Time
	// expires_at is capped at created_at + 30 days (1 day from now), not now()+7d
	ownerRead(t, f.owner, `SELECT created_at, expires_at, last_seen_at FROM web_sessions WHERE id_hash='\x51'`, &created, &expires, &last)
	if !expires.Equal(created.Add(30 * 24 * time.Hour)) {
		t.Errorf("expires_at %v not capped at created+30d (%v)", expires, created)
	}
	if time.Since(last) > time.Minute {
		t.Error("last_seen_at not refreshed")
	}
	var tok, dev, usr string
	var scopes []string
	if err := f.d.WithSystem(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT * FROM auth_device_token('\x52')`).Scan(&tok, &dev, &usr, &scopes)
	}); err != nil {
		t.Fatal(err)
	}
	if dev != "00000000-0000-0000-0000-00000000000a" || usr != f.userA || len(scopes) != 1 || scopes[0] != "ingest:write" {
		t.Fatalf("auth_device_token: %s %s %v", dev, usr, scopes)
	}
	var used time.Time
	ownerRead(t, f.owner, `SELECT last_used_at FROM device_tokens WHERE hash='\x52'`, &used)
	if time.Since(used) > time.Minute {
		t.Error("last_used_at not updated")
	}
}

func ownerRead(t *testing.T, ownerURL, q string, dest ...any) {
	t.Helper()
	ctx := context.Background()
	c, err := pgx.Connect(ctx, ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(ctx) }()
	if err := c.QueryRow(ctx, q).Scan(dest...); err != nil {
		t.Fatal(err)
	}
}

func TestPartsAreInvisibleAndImmutableToOtherUsers(t *testing.T) {
	f := setup(t)
	testdb.Exec(t, f.owner, `INSERT INTO raw_line_parts(user_id,source_file_id,"offset",device_id,idx,count,total_len,sha256,chunk) SELECT $1,id,0,gen_random_uuid(),0,2,9,'\x01','\x7b' FROM source_files`, f.userA)
	var n int
	err := f.d.WithUser(context.Background(), f.userB, func(tx pgx.Tx) error {
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM raw_line_parts`).Scan(&n); err != nil {
			return err
		}
		_, err := tx.Exec(context.Background(), `DELETE FROM raw_line_parts`)
		return err
	})
	if err != nil || n != 0 {
		t.Fatalf("B sees %d parts, err %v", n, err)
	}
	ownerRead(t, f.owner, `SELECT count(*) FROM raw_line_parts`, &n)
	if n != 1 {
		t.Fatalf("A's part was deleted by B: %d", n)
	}
}

func TestStalePartsAreCollected(t *testing.T) {
	f := setup(t)
	testdb.Exec(t, f.owner, `INSERT INTO raw_line_parts(user_id,source_file_id,"offset",device_id,idx,count,total_len,sha256,chunk,received_at)
		SELECT $1,id,0,gen_random_uuid(),i,3,9,'\x01','\x7b',now() - (i * interval '5 days') FROM source_files, generate_series(0,2) i`, f.userA)
	err := f.d.WithSystem(context.Background(), func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `SELECT gc_line_parts()`)
		return err
	})
	var n int
	ownerRead(t, f.owner, `SELECT count(*) FROM raw_line_parts`, &n)
	if err != nil || n != 2 { // 0 and 5 days old stay, 10 days old goes
		t.Fatalf("remaining %d, err %v", n, err)
	}
}
