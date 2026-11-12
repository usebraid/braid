// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package state_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/usebraid/braid/internal/daemon/state"
	"github.com/usebraid/braid/internal/protocol"
)

func TestStateRoundTrip(t *testing.T) {
	ctx := context.Background()
	p := filepath.Join(t.TempDir(), "sub", "state.db")
	d, err := state.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(p); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("mode: %v %v", st, err)
	}
	f := state.File{RelPath: "p/a.jsonl", HeadHash: "h", Kind: protocol.KindAppend, AckedOffset: 10, SizeSeen: 30, FileID: "1:2"}
	if err := d.Put(ctx, f); err != nil {
		t.Fatal(err)
	}
	got, ok, err := d.Get(ctx, "p/a.jsonl", "h")
	if err != nil || !ok || got.AckedOffset != 10 {
		t.Fatalf("%+v %v %v", got, ok, err)
	}
	f.AckedOffset = 30
	f.LastAckAt = time.Now().UTC().Truncate(time.Second)
	_ = d.Put(ctx, f)
	b, _ := d.Backlog(ctx)
	if b.Files != 0 || b.Bytes != 0 || !b.LastAck.Equal(f.LastAckAt) {
		t.Fatalf("%+v", b)
	}
	_ = d.Put(ctx, state.File{RelPath: "p/b.jsonl", HeadHash: "h2", Kind: protocol.KindAppend, AckedOffset: 5, SizeSeen: 25})
	_ = d.Put(ctx, state.File{RelPath: "p/c.json", Kind: protocol.KindSnapshot, SizeSeen: -1})
	b, _ = d.Backlog(ctx)
	if b.Files != 2 || b.Bytes != 20 || !b.OldestUnacked.IsZero() {
		t.Fatalf("%+v", b)
	}
	_ = d.SetSetting(ctx, "server_url", "https://x")
	_ = d.Close()
	d2, _ := state.Open(p)
	defer func() { _ = d2.Close() }()
	if v, ok, _ := d2.Setting(ctx, "server_url"); !ok || v != "https://x" {
		t.Fatal("setting not persisted")
	}
	all, _ := d2.List(ctx)
	if len(all) != 3 {
		t.Fatalf("%d files", len(all))
	}
	if err := d2.Delete(ctx, "p/c.json", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := d2.Get(ctx, "p/c.json", ""); ok {
		t.Fatal("not deleted")
	}
}

func TestDeviceScopedPersistsAndOldDBUpgrades(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.db")
	old, err := sql.Open("sqlite", "file:"+p) // driver registered by the state package
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE files (rel_path TEXT NOT NULL, head_hash TEXT NOT NULL, kind TEXT NOT NULL, acked_offset INTEGER NOT NULL DEFAULT 0,
		  content_hash TEXT, file_id TEXT, size_seen INTEGER NOT NULL DEFAULT 0, spool_path TEXT, original_gone INTEGER NOT NULL DEFAULT 0,
		  last_ack_at TEXT, PRIMARY KEY (rel_path, head_hash))`,
		`INSERT INTO files(rel_path, head_hash, kind, acked_offset) VALUES ('a.jsonl','h','append',5)`,
	} {
		if _, err := old.ExecContext(context.Background(), q); err != nil {
			t.Fatal(err)
		}
	}
	_ = old.Close()
	st, err := state.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	f, ok, err := st.Get(ctx, "a.jsonl", "h")
	if err != nil || !ok || f.DeviceScoped || f.AckedOffset != 5 {
		t.Fatalf("%+v %v %v", f, ok, err)
	}
	f.DeviceScoped = true
	if err := st.Put(ctx, f); err != nil {
		t.Fatal(err)
	}
	if g, _, _ := st.Get(ctx, "a.jsonl", "h"); !g.DeviceScoped {
		t.Fatal("device_scoped not persisted")
	}
}
