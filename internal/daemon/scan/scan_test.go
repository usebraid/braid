// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package scan_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/usebraid/braid/internal/daemon/scan"
	"github.com/usebraid/braid/internal/protocol"
)

func TestClassify(t *testing.T) {
	cases := map[string]struct {
		k  protocol.Kind
		ok bool
	}{
		"p/s.jsonl":                       {protocol.KindAppend, true},
		"p/s/subagents/agent-a.jsonl":     {protocol.KindAppend, true},
		"p/s/subagents/agent-a.meta.json": {protocol.KindSnapshot, true},
		"p/s/custom-title.json":           {protocol.KindSnapshot, true},
		"p/memory/MEMORY.md":              {"", false},
		"p/.DS_Store":                     {"", false},
		"p/s.jsonl.tmp":                   {"", false},
		"p/memory/x.jsonl":                {"", false},
		"p/.hidden/x.jsonl":               {"", false},
	}
	for p, want := range cases {
		k, ok := scan.Classify(p)
		if k != want.k || ok != want.ok {
			t.Errorf("%s: %v %v", p, k, ok)
		}
	}
}

func TestWalk(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o750)
		_ = os.WriteFile(p, []byte("{}\n"), 0o600)
	}
	mk("b/s.jsonl")
	mk("a/s/subagents/agent-x.jsonl")
	mk("a/s/subagents/agent-x.meta.json")
	mk("a/memory/MEMORY.md")
	_ = os.Symlink(filepath.Join(root, "b/s.jsonl"), filepath.Join(root, "b/link.jsonl"))
	es, err := scan.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range es {
		got = append(got, e.RelPath)
	}
	want := []string{"a/s/subagents/agent-x.jsonl", "a/s/subagents/agent-x.meta.json", "b/s.jsonl"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%v", got)
	}
	if es, err := scan.Walk(filepath.Join(root, "missing")); err != nil || len(es) != 0 {
		t.Fatalf("missing root: %v %v", es, err)
	}
}

func TestWalkSymlinkedRoot(t *testing.T) {
	target := t.TempDir()
	_ = os.MkdirAll(filepath.Join(target, "a"), 0o750)
	_ = os.WriteFile(filepath.Join(target, "a", "s.jsonl"), []byte("{}\n"), 0o600)
	link := filepath.Join(t.TempDir(), "projects")
	if err := os.Symlink(target, link); err != nil {
		t.Skip(err)
	}
	es, err := scan.Walk(link)
	if err != nil || len(es) != 1 || es[0].RelPath != "a/s.jsonl" {
		t.Fatalf("%v %v", es, err)
	}
}

func TestWalkSkipsUnreadableDir(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs unix non-root")
	}
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "ok"), 0o750)
	_ = os.MkdirAll(filepath.Join(root, "bad"), 0o750)
	_ = os.WriteFile(filepath.Join(root, "ok", "s.jsonl"), []byte("{}\n"), 0o600)
	_ = os.WriteFile(filepath.Join(root, "bad", "s.jsonl"), []byte("{}\n"), 0o600)
	if err := os.Chmod(filepath.Join(root, "bad"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "bad"), 0o700) }) //nolint:gosec // restore for TempDir cleanup
	es, err := scan.Walk(root)
	if err == nil || len(es) != 1 || es[0].RelPath != "ok/s.jsonl" {
		t.Fatalf("%v %v", es, err)
	}
}
