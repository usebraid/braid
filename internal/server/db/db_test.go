// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/usebraid/braid/internal/server/db"
	"github.com/usebraid/braid/internal/testdb"
)

type fixture struct {
	d     *db.DB
	owner string
	userA string
	userB string
	fileA int64 //nolint:unused // field from the binding fixture
}

func setup(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	ownerURL, appURL := testdb.New(t)
	if err := db.Migrate(ctx, ownerURL); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	d, err := db.Open(ctx, appURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	f := fixture{d: d, owner: ownerURL}
	f.userA = upsert(t, d, "sub-a", false)
	f.userB = upsert(t, d, "sub-b", false)
	// device + source file + line for A, created as owner
	testdb.Exec(t, ownerURL, `INSERT INTO devices(id,user_id,name,created_at) VALUES ('00000000-0000-0000-0000-00000000000a',$1,'a',now())`, f.userA)
	testdb.Exec(t, ownerURL, `INSERT INTO source_files(user_id,device_id,rel_path,head_hash,kind,first_seen,last_seen) VALUES ($1,'00000000-0000-0000-0000-00000000000a','p/a.jsonl','h','append',now(),now())`, f.userA)
	testdb.Exec(t, ownerURL, `INSERT INTO raw_lines(user_id,source_file_id,"offset",line_hash,line,received_at) SELECT $1,id,0,'\x01','\x7b7d',now() FROM source_files`, f.userA)
	return f
}

func upsert(t *testing.T, d *db.DB, sub string, admin bool) string {
	t.Helper()
	var id string
	err := d.WithSystem(context.Background(), func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT user_id FROM upsert_oidc_user('https://idp', $1, $1 || '@x', $1, $2)`, sub, admin).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func count(t *testing.T, d *db.DB, user, table string) int {
	t.Helper()
	var n int
	run := d.WithSystem
	if user != "" {
		run = func(ctx context.Context, fn func(pgx.Tx) error) error { return d.WithUser(ctx, user, fn) }
	}
	if err := run(context.Background(), func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRLSIsolation(t *testing.T) {
	f := setup(t)
	for _, table := range []string{"devices", "source_files", "raw_lines"} {
		if n := count(t, f.d, f.userA, table); n != 1 {
			t.Errorf("A sees %d rows in %s, want 1", n, table)
		}
		if n := count(t, f.d, f.userB, table); n != 0 {
			t.Errorf("B sees %d rows in %s, want 0", n, table)
		}
		if n := count(t, f.d, "", table); n != 0 {
			t.Errorf("no user sees %d rows in %s, want 0", n, table)
		}
	}
	if n := count(t, f.d, f.userB, "users"); n != 1 {
		t.Errorf("B sees %d users, want only self", n)
	}
}

func TestRLSBlocksForeignInsert(t *testing.T) {
	f := setup(t)
	err := f.d.WithUser(context.Background(), f.userB, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO source_files(user_id,device_id,rel_path,head_hash,kind,first_seen,last_seen) VALUES ($1,'00000000-0000-0000-0000-00000000000a','x.jsonl','h2','append',now(),now())`, f.userA)
		return err
	})
	if err == nil {
		t.Fatal("B inserted a row owned by A")
	}
}

func TestRawTablesAppendOnly(t *testing.T) {
	f := setup(t)
	for _, q := range []string{`UPDATE raw_lines SET line = '\x00'`, `DELETE FROM raw_lines`, `UPDATE raw_snapshots SET content = '\x00'`, `DELETE FROM raw_snapshots`, `DELETE FROM audit_log`, `INSERT INTO audit_log(at,action) VALUES (now(),'login')`} {
		err := f.d.WithUser(context.Background(), f.userA, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), q)
			return err
		})
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Errorf("%s: want insufficient_privilege, got %v", q, err)
		}
	}
}

func TestDeviceCodesNotReadableByApp(t *testing.T) {
	f := setup(t)
	err := f.d.WithSystem(context.Background(), func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `SELECT * FROM device_codes`)
		return err
	})
	if err == nil {
		t.Fatal("braid_app can read device_codes")
	}
}

func TestAdminFunctionsRequireAdmin(t *testing.T) {
	f := setup(t)
	admin := upsert(t, f.d, "sub-admin", true)
	call := func(user, q string, args ...any) error {
		return f.d.WithUser(context.Background(), user, func(tx pgx.Tx) error {
			rows, err := tx.Query(context.Background(), q, args...)
			if err != nil {
				return err
			}
			rows.Close()
			return rows.Err()
		})
	}
	if err := call(f.userA, `SELECT * FROM admin_list_users()`); err == nil {
		t.Error("non-admin listed users")
	}
	if err := call(f.userA, `SELECT admin_set_disabled($1, true)`, f.userB); err == nil {
		t.Error("non-admin disabled user")
	}
	if err := call(admin, `SELECT * FROM admin_list_users()`); err != nil {
		t.Errorf("admin list: %v", err)
	}
	if err := call(admin, `SELECT admin_set_disabled($1, true)`, admin); err == nil {
		t.Error("admin disabled self")
	}
	if err := call(admin, `SELECT admin_delete_user($1)`, admin); err == nil {
		t.Error("admin deleted self")
	}
	// aggregates only: the result columns contain no content columns
	err := f.d.WithUser(context.Background(), admin, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT * FROM admin_list_users()`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for _, fd := range rows.FieldDescriptions() {
			if fd.Name == "line" || fd.Name == "content" {
				t.Errorf("admin_list_users exposes %s", fd.Name)
			}
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDisableRevokesSessionsAndTokens(t *testing.T) {
	f := setup(t)
	admin := upsert(t, f.d, "sub-admin", true)
	testdb.Exec(t, f.owner, `INSERT INTO web_sessions(id_hash,user_id,created_at,last_seen_at,expires_at) VALUES ('\x01',$1,now(),now(),now()+interval '1 day')`, f.userA)
	testdb.Exec(t, f.owner, `INSERT INTO device_tokens(id,device_id,user_id,hash,scopes,created_at) VALUES (gen_random_uuid(),'00000000-0000-0000-0000-00000000000a',$1,'\x02','{ingest:write}',now())`, f.userA)
	if err := f.d.WithUser(context.Background(), admin, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `SELECT admin_set_disabled($1, true)`, f.userA)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = f.d.WithSystem(context.Background(), func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM auth_session('\x01')`).Scan(&n)
	})
	if n != 0 {
		t.Error("session of disabled user still valid")
	}
	_ = f.d.WithSystem(context.Background(), func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM auth_device_token('\x02')`).Scan(&n)
	})
	if n != 0 {
		t.Error("token of disabled user still valid")
	}
}

func TestDeviceTokenExpiresAfter90DaysUnused(t *testing.T) {
	f := setup(t)
	testdb.Exec(t, f.owner, `INSERT INTO device_tokens(id,device_id,user_id,hash,scopes,created_at,last_used_at) VALUES (gen_random_uuid(),'00000000-0000-0000-0000-00000000000a',$1,'\x03','{ingest:write}',now()-interval '200 days',now()-interval '91 days')`, f.userA)
	var n int
	_ = f.d.WithSystem(context.Background(), func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM auth_device_token('\x03')`).Scan(&n)
	})
	if n != 0 {
		t.Fatal("expired token accepted")
	}
	if c := count(t, f.d, f.userA, "audit_log WHERE action = 'token_expired'"); c != 1 {
		t.Fatalf("token_expired audit rows: %d", c)
	}
}

func TestDeviceCodeFlowFunctions(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	sys := func(q string, args ...any) (status string) {
		_ = f.d.WithSystem(ctx, func(tx pgx.Tx) error {
			var dev *string
			var iv int
			return tx.QueryRow(ctx, q, args...).Scan(&status, &dev, &iv)
		})
		return
	}
	if err := f.d.WithSystem(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT device_code_create('\x10','\x20','{"hostname":"mac","os":"darwin","arch":"arm64","os_user":"jan","daemon_version":"0.1.0"}','127.0.0.1')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if s := sys(`SELECT * FROM device_code_poll('\x10','\x30')`); s != "authorization_pending" {
		t.Fatalf("first poll: %s", s)
	}
	if s := sys(`SELECT * FROM device_code_poll('\x10','\x30')`); s != "slow_down" {
		t.Fatalf("fast poll: %s", s)
	}
	testdb.Exec(t, f.owner, `UPDATE device_codes SET last_poll_at = now() - interval '1 hour'`)
	var dev *string
	if err := f.d.WithUser(ctx, f.userB, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT device_code_decide('\x20', true, 'Laptop')`).Scan(&dev)
	}); err != nil || dev == nil {
		t.Fatalf("approve: %v %v", dev, err)
	}
	if s := sys(`SELECT * FROM device_code_poll('\x10','\x30')`); s != "ok" {
		t.Fatalf("approved poll: %s", s)
	}
	testdb.Exec(t, f.owner, `UPDATE device_codes SET last_poll_at = now() - interval '1 hour'`)
	if s := sys(`SELECT * FROM device_code_poll('\x10','\x31')`); s != "invalid_grant" {
		t.Fatalf("reuse: %s", s)
	}
	if s := sys(`SELECT * FROM device_code_poll('\x99','\x32')`); s != "invalid_grant" {
		t.Fatalf("unknown: %s", s)
	}
	if n := count(t, f.d, f.userB, "devices"); n != 1 {
		t.Fatalf("B devices: %d", n)
	}
	if n := count(t, f.d, f.userB, "device_tokens"); n != 1 {
		t.Fatalf("B tokens: %d", n)
	}
	// expired code
	_ = f.d.WithSystem(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT device_code_create('\x11','\x21','{"hostname":"h","os":"linux","arch":"amd64","os_user":"u","daemon_version":"0.1.0"}',NULL)`)
		return err
	})
	testdb.Exec(t, f.owner, `UPDATE device_codes SET expires_at = now() - interval '1 second' WHERE device_code_hash = '\x11'`)
	if s := sys(`SELECT * FROM device_code_poll('\x11','\x33')`); s != "expired_token" {
		t.Fatalf("expired: %s", s)
	}
	var none *string
	_ = f.d.WithUser(ctx, f.userA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT device_code_decide('\x21', true, 'x')`).Scan(&none)
	})
	if none != nil {
		t.Fatal("expired code approved")
	}
}
