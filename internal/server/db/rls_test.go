// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
		"disable rls":               `ALTER TABLE raw_lines DISABLE ROW LEVEL SECURITY`,
	}
	for name, q := range cases {
		if err := userExec(f, f.userA, q); err == nil {
			t.Errorf("%s: statement succeeded", name)
		}
	}
}

// A user may not attach their own rows to another user's device or file (composite FKs).
func TestNoCrossUserParent(t *testing.T) {
	f := setup(t)
	err := userExec(f, f.userB, `INSERT INTO source_files(user_id,device_id,rel_path,head_hash,kind,first_seen,last_seen) VALUES ($1,'00000000-0000-0000-0000-00000000000a','x','h','append',now(),now())`, f.userB)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("want foreign_key_violation, got %v", err)
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
