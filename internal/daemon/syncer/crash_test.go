// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package syncer_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usebraid/braid/internal/daemon/client"
)

// A crash between Spool.Retire (rename) and the state write must not orphan unacked lines.
func TestCrashAfterRetireBeforeStateWrite(t *testing.T) {
	fs := newFakeServer()
	e, root := newEngine(t, fs)
	appendTo(t, root, "p/a.jsonl", "h\n1\n2\n")
	fs.failNext = &client.RetryableError{Status: 503}
	_, _ = e.Pass(context.Background()) // spooled, nothing acked
	_ = os.Remove(filepath.Join(root, "p/a.jsonl"))
	row, _, _ := e.State.Get(context.Background(), "p/a.jsonl", sum([]byte("h")))
	if old, err := e.Spool.Retire("p/a.jsonl", row.FileID); err != nil || old == "" { // the process dies here
		t.Fatal(old, err)
	}
	if _, err := e.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(fs.lines("p/a.jsonl"), ","); got != "h,1,2" {
		t.Fatalf("unacked lines orphaned: %q", got)
	}
}

func TestCrashAfterRetireBeforeStateWriteReplaced(t *testing.T) {
	fs := newFakeServer()
	e, root := newEngine(t, fs)
	appendTo(t, root, "p/a.jsonl", "h\nold1\nold2\n")
	fs.failNext = &client.RetryableError{Status: 503}
	_, _ = e.Pass(context.Background())
	row, _, _ := e.State.Get(context.Background(), "p/a.jsonl", sum([]byte("h")))
	_ = os.Remove(filepath.Join(root, "p/a.jsonl"))
	appendTo(t, root, "p/b.tmp", "keep inode busy")
	appendTo(t, root, "p/a.jsonl", "h\nnew1\n")
	if _, err := e.Spool.Retire("p/a.jsonl", row.FileID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(fs.lines("p/a.jsonl"), ","); !strings.Contains(got, "old2") {
		t.Fatalf("old generation tail lost: %q", got)
	}
}
