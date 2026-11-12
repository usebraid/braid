// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package spool_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/usebraid/braid/internal/daemon/reader"
	"github.com/usebraid/braid/internal/daemon/spool"
)

func TestHardlinkSurvivesDeletionAndSeesAppends(t *testing.T) {
	root, sp := t.TempDir(), spool.New(t.TempDir())
	abs := filepath.Join(root, "a.jsonl")
	_ = os.WriteFile(abs, []byte("l1\n"), 0o600)
	id, _ := reader.FileID(abs)
	p, copied, err := sp.Ensure("p/a.jsonl", abs, id)
	if err != nil || copied {
		t.Fatalf("%v %v", copied, err)
	}
	f, _ := os.OpenFile(abs, os.O_APPEND|os.O_WRONLY, 0) //nolint:gosec // test path
	_, _ = f.WriteString("l2\n")
	_ = f.Close()
	_ = os.Remove(abs)
	b, err := os.ReadFile(p) //nolint:gosec // test path
	if err != nil || string(b) != "l1\nl2\n" {
		t.Fatalf("%q %v", b, err)
	}
	if err := sp.Remove(p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(sp.Dir, "p")); !os.IsNotExist(err) {
		t.Fatal("empty dir not pruned")
	}
}

func TestCopyFallback(t *testing.T) {
	root := t.TempDir()
	sp := spool.New(t.TempDir())
	spool.SetLinkForTest(sp, func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.EXDEV} })
	abs := filepath.Join(root, "a.jsonl")
	_ = os.WriteFile(abs, []byte("l1\n"), 0o600)
	id, _ := reader.FileID(abs)
	p, copied, err := sp.Ensure("p/a.jsonl", abs, id)
	if err != nil || !copied {
		t.Fatalf("%v %v", copied, err)
	}
	_ = os.WriteFile(abs, []byte("l1\nl2\n"), 0o600)
	_, _, _ = sp.Ensure("p/a.jsonl", abs, id)
	if b, _ := os.ReadFile(p); string(b) != "l1\nl2\n" { //nolint:gosec // test path
		t.Fatalf("copy not refreshed: %q", b)
	}
}

func TestRetireKeepsOldContent(t *testing.T) {
	root, sp := t.TempDir(), spool.New(t.TempDir())
	abs := filepath.Join(root, "a.json")
	_ = os.WriteFile(abs, []byte("old"), 0o600)
	id1, _ := reader.FileID(abs)
	_, _, _ = sp.Ensure("p/a.json", abs, id1)
	old, err := sp.Retire("p/a.json", id1)
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(root, "t")
	_ = os.WriteFile(tmp, []byte("new"), 0o600)
	_ = os.Rename(tmp, abs)
	id2, _ := reader.FileID(abs)
	p, _, _ := sp.Ensure("p/a.json", abs, id2)
	if b, _ := os.ReadFile(old); string(b) != "old" { //nolint:gosec // test path
		t.Fatalf("old: %q", b)
	}
	if b, _ := os.ReadFile(p); string(b) != "new" { //nolint:gosec // test path
		t.Fatalf("new: %q", b)
	}
}

func TestRejectsEscapingPaths(t *testing.T) {
	sp := spool.New(t.TempDir())
	for _, rel := range []string{"../x", "a/../../x", "/abs"} {
		if _, _, err := sp.Ensure(rel, "x", "id"); err == nil {
			t.Errorf("Ensure(%q) accepted", rel)
		}
		if _, err := sp.Retire(rel, "id"); err == nil {
			t.Errorf("Retire(%q) accepted", rel)
		}
	}
}
