// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package db_test

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/usebraid/braid/internal/server/db"
	"github.com/usebraid/braid/internal/testdb"
)

func strs(t *testing.T, owner, q string) []string {
	t.Helper()
	ctx := context.Background()
	c, err := pgx.Connect(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(ctx) }()
	rows, err := c.Query(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func TestMigration0004MergesDevicesWithoutLoss(t *testing.T) {
	ctx := context.Background()
	owner, _ := testdb.NewUnmigrated(t)
	if err := db.MigrateTo(ctx, owner, "0003_definer_guards.sql"); err != nil {
		t.Fatal(err)
	}
	const u = "00000000-0000-0000-0000-0000000000a1"
	const d1, d2 = "00000000-0000-0000-0000-0000000000d1", "00000000-0000-0000-0000-0000000000d2"
	x := func(q string, a ...any) { testdb.Exec(t, owner, q, a...) }
	x(`INSERT INTO users(id, oidc_issuer, oidc_sub) VALUES ($1,'i','s')`, u)
	x(`INSERT INTO devices(id,user_id,name) VALUES ($1,$3,'one'),($2,$3,'two')`, d1, d2, u)
	file := func(dev, rel, head, kind string, end int) {
		x(`INSERT INTO source_files(user_id,device_id,rel_path,head_hash,kind,end_offset,first_seen,last_seen)
		   VALUES ($1,$2,$3,$4,$5,$6,now(),now())`, u, dev, rel, head, kind, end)
	}
	line := func(dev, rel, head string, off int, text string) {
		x(`INSERT INTO raw_lines(user_id,source_file_id,"offset",line_hash,line)
		   SELECT $1::uuid, id, $5::bigint, $6::bytea, $6::bytea FROM source_files WHERE device_id=$2 AND rel_path=$3 AND head_hash=$4`,
			u, dev, rel, head, off, []byte(text))
	}
	snap := func(dev, rel, text string, ago string) {
		x(`INSERT INTO raw_snapshots(user_id,source_file_id,content_hash,content,received_at)
		   SELECT $1::uuid, id, $4::bytea, $4::bytea, now() - $5::interval FROM source_files WHERE device_id=$2 AND rel_path=$3`,
			u, dev, rel, []byte(text), ago)
	}
	// same.jsonl: d1 holds offsets 0,8; d2 holds 0,8 (identical) and 16.
	file(d1, "p/same.jsonl", "h1", "append", 16)
	line(d1, "p/same.jsonl", "h1", 0, "a")
	line(d1, "p/same.jsonl", "h1", 8, "b")
	file(d2, "p/same.jsonl", "h1", "append", 24)
	line(d2, "p/same.jsonl", "h1", 0, "a")
	line(d2, "p/same.jsonl", "h1", 8, "b")
	line(d2, "p/same.jsonl", "h1", 16, "c")
	// conf.jsonl: both devices hold a different line at offset 0.
	file(d1, "p/conf.jsonl", "h2", "append", 2)
	line(d1, "p/conf.jsonl", "h2", 0, "x")
	file(d2, "p/conf.jsonl", "h2", "append", 2)
	line(d2, "p/conf.jsonl", "h2", 0, "y")
	// solo.jsonl: one device only.
	file(d1, "p/solo.jsonl", "h3", "append", 2)
	line(d1, "p/solo.jsonl", "h3", 0, "q")
	// snapshots: d1 v1; d2 v1 (same content) then v2.
	file(d1, "p/s/x.meta.json", "", "snapshot", 0)
	snap(d1, "p/s/x.meta.json", "v1", "3 hours")
	file(d2, "p/s/x.meta.json", "", "snapshot", 0)
	snap(d2, "p/s/x.meta.json", "v1", "2 hours")
	snap(d2, "p/s/x.meta.json", "v2", "1 hour")

	const lineSet = `SELECT f.rel_path||'|'||l."offset"||'|'||encode(l.line_hash,'hex') FROM raw_lines l JOIN source_files f ON f.id=l.source_file_id`
	before := strs(t, owner, `SELECT DISTINCT s FROM (`+lineSet+`) x(s) ORDER BY 1`)

	if err := db.Migrate(ctx, owner); err != nil {
		t.Fatal(err)
	}

	if after := strs(t, owner, `SELECT DISTINCT s FROM (`+lineSet+`) x(s) ORDER BY 1`); !reflect.DeepEqual(before, after) {
		t.Fatalf("line set changed:\nbefore %v\nafter  %v", before, after)
	}
	if got := strs(t, owner, `SELECT count(*)::text FROM source_files WHERE rel_path='p/same.jsonl'`); got[0] != "1" {
		t.Fatalf("same.jsonl rows: %v", got)
	}
	if got := strs(t, owner, `SELECT end_offset::text FROM source_files WHERE rel_path='p/same.jsonl' AND scope_device_id IS NULL`); !slices.Equal(got, []string{"24"}) {
		t.Fatalf("same.jsonl end: %v", got)
	}
	got := strs(t, owner, `SELECT l.device_id::text FROM raw_lines l JOIN source_files f ON f.id=l.source_file_id WHERE f.rel_path='p/same.jsonl' ORDER BY l."offset"`)
	if !slices.Equal(got, []string{d1, d1, d2}) { // first delivering device stays on duplicates
		t.Fatalf("same.jsonl devices: %v", got)
	}
	if got := strs(t, owner, `SELECT coalesce(scope_device_id::text,'-') FROM source_files WHERE rel_path='p/conf.jsonl' ORDER BY id`); !slices.Equal(got, []string{"-", d2}) {
		t.Fatalf("conf.jsonl scopes: %v", got)
	}
	if got := strs(t, owner, `SELECT count(*)::text FROM source_files WHERE rel_path='p/solo.jsonl' AND scope_device_id IS NULL`); got[0] != "1" {
		t.Fatalf("solo: %v", got)
	}
	if got := strs(t, owner, `SELECT convert_from(s.content,'UTF8') FROM raw_snapshots s JOIN source_files f ON f.id=s.source_file_id WHERE f.rel_path='p/s/x.meta.json' ORDER BY s.received_at, s.id`); !slices.Equal(got, []string{"v1", "v2"}) {
		t.Fatalf("snapshot history: %v", got)
	}
	if got := strs(t, owner, `SELECT count(*)::text FROM information_schema.columns WHERE table_name='source_files' AND column_name='device_id'`); got[0] != "0" {
		t.Fatal("source_files.device_id still exists")
	}
	if got := strs(t, owner, `SELECT count(*)::text FROM raw_lines WHERE device_id IS NULL`); got[0] != "0" {
		t.Fatal("raw_lines without device")
	}
}
