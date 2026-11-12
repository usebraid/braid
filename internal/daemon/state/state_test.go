// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package state_test

import (
	"context"
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
