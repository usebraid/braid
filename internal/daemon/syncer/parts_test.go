// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package syncer_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/usebraid/braid/internal/daemon/client"
)

func TestOversizedLineIsChunkedAndTheRestSyncs(t *testing.T) {
	fs := newFakeServer()
	e, root := newEngine(t, fs)
	big := strings.Repeat("é\"", 2<<20) // 8 MiB of text that escapes
	appendTo(t, root, "p/a.jsonl", "head\nbefore\n"+big+"\nafter\n")
	if _, err := e.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := fs.lines("p/a.jsonl")
	if len(got) != 4 || got[2] != big || got[3] != "after" {
		t.Fatalf("lines: %d", len(got))
	}
	if sk, _ := e.State.ListSkipped(context.Background()); len(sk) != 0 {
		t.Fatalf("skipped %v", sk)
	}
}

func TestChunkedLineResumesAfterARetryableError(t *testing.T) {
	fs := newFakeServer()
	fs.failOnPart = 2
	e, root := newEngine(t, fs)
	big := strings.Repeat("x", 4<<20)
	appendTo(t, root, "p/a.jsonl", "head\n"+big+"\n")
	var rerr *client.RetryableError
	if _, err := e.Pass(context.Background()); !errors.As(err, &rerr) {
		t.Fatalf("want retryable, got %v", err)
	}
	if _, err := e.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fs.lines("p/a.jsonl"); len(got) != 2 || got[1] != big {
		t.Fatal("line not stored")
	}
	if fs.partCalls[0] != 1 || fs.partCalls[1] != 1 {
		t.Fatalf("stored chunks were sent again: %v", fs.partCalls)
	}
}

func TestOversizedSnapshotIsChunked(t *testing.T) {
	fs := newFakeServer()
	e, root := newEngine(t, fs)
	content := `{"k":"` + strings.Repeat("v", 3<<20) + `"}`
	appendTo(t, root, "p/a/x.meta.json", content)
	if _, err := e.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := fs.snaps["p/a/x.meta.json"]; len(s) != 1 || s[0] != sum([]byte(content)) {
		t.Fatalf("snapshot %v", s)
	}
}

func TestLineAboveTheHardLimitIsSkippedAndKept(t *testing.T) {
	fs := newFakeServer()
	e, root := newEngine(t, fs)
	e.MaxLine = 2 << 20
	big := strings.Repeat("z", 3<<20)
	appendTo(t, root, "p/a.jsonl", "head\nbefore\n"+big+"\nafter\n")
	if _, err := e.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fs.lines("p/a.jsonl"); len(got) != 3 || got[1] != "before" || got[2] != "after" {
		t.Fatalf("lines %d", len(got))
	}
	sk, _ := e.State.ListSkipped(context.Background())
	if len(sk) != 1 || sk[0].RelPath != "p/a.jsonl" || sk[0].Offset != 12 || sk[0].Length != int64(len(big))+1 || sk[0].SHA256 != sum([]byte(big)) {
		t.Fatalf("skipped %+v", sk)
	}
	rows, _ := e.State.List(context.Background())
	if len(rows) != 1 || rows[0].SpoolPath == "" {
		t.Fatalf("row %+v", rows)
	}
	if _, err := os.Stat(rows[0].SpoolPath); err != nil {
		t.Fatalf("spool copy gone: %v", err)
	}
	_ = os.Remove(root + "/p/a.jsonl") // even with the original gone, the copy stays
	if _, err := e.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, _ = e.State.List(context.Background())
	if len(rows) != 1 {
		t.Fatalf("row dropped: %+v", rows)
	}
	if _, err := os.Stat(rows[0].SpoolPath); err != nil { // the entry may have been moved aside, never removed
		t.Fatalf("spool copy collected: %v", err)
	}
}
