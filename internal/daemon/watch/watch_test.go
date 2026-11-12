// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package watch_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/usebraid/braid/internal/daemon/watch"
)

func TestTriggersOnWriteAndNewDir(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := watch.Triggers(ctx, root, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	expect := func(what string) {
		select {
		case <-ch:
		case <-time.After(3 * time.Second):
			t.Fatalf("no trigger for %s", what)
		}
	}
	expect("start")
	_ = os.MkdirAll(filepath.Join(root, "p", "s", "subagents"), 0o750)
	expect("mkdir")
	time.Sleep(100 * time.Millisecond) // let the watcher add the new dir
	_ = os.WriteFile(filepath.Join(root, "p", "s", "subagents", "agent-a.jsonl"), []byte("x\n"), 0o600)
	expect("write in new dir")
}

func TestTriggersRescan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, _ := watch.Triggers(ctx, filepath.Join(t.TempDir(), "missing"), 50*time.Millisecond)
	for i := 0; i < 3; i++ {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatal("rescan trigger missing")
		}
	}
}
