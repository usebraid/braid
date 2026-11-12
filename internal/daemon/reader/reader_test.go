// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package reader_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usebraid/braid/internal/daemon/reader"
	"github.com/usebraid/braid/internal/protocol"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPartialLineNotReturned(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jsonl")
	write(t, p, "l1\nl2\nhal")
	lines, next, err := reader.ReadLines(p, 0, 1<<20)
	if err != nil || len(lines) != 2 || next != 6 {
		t.Fatalf("%q %d %v", lines, next, err)
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0) //nolint:gosec // test path
	_, _ = f.WriteString("f\n")
	_ = f.Close()
	lines, next, _ = reader.ReadLines(p, 6, 1<<20)
	if len(lines) != 1 || string(lines[0]) != "half" || next != 11 {
		t.Fatalf("%q %d", lines, next)
	}
}

func TestConcurrentAppendNeverSkips(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jsonl")
	write(t, p, "")
	done := make(chan struct{})
	go func() {
		f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0) //nolint:gosec // test path
		for i := 0; i < 2000; i++ {
			s := fmt.Sprintf(`{"i":%d,"pad":"%s"}`+"\n", i, strings.Repeat("x", i%97))
			// write in two halves to provoke partial reads
			_, _ = f.WriteString(s[:len(s)/2])
			_, _ = f.WriteString(s[len(s)/2:])
		}
		_ = f.Close()
		close(done)
	}()
	var got []string
	var off int64
	for {
		lines, next, err := reader.ReadLines(p, off, 4096)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range lines {
			got = append(got, string(l))
		}
		off = next
		select {
		case <-done:
			if len(lines) == 0 {
				lines, _, _ = reader.ReadLines(p, off, 1<<30)
				for _, l := range lines {
					got = append(got, string(l))
				}
				if len(got) != 2000 {
					t.Fatalf("got %d lines", len(got))
				}
				for i, l := range got {
					if !strings.HasPrefix(l, fmt.Sprintf(`{"i":%d,`, i)) {
						t.Fatalf("line %d = %q", i, l)
					}
				}
				return
			}
		default:
		}
	}
}

func TestOversizedLineReturnedAlone(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jsonl")
	big := strings.Repeat("b", 5000)
	write(t, p, big+"\nsmall\n")
	lines, next, _ := reader.ReadLines(p, 0, 1000)
	if len(lines) != 1 || len(lines[0]) != 5000 || next != 5001 {
		t.Fatalf("%d lines next %d", len(lines), next)
	}
	lines, _, _ = reader.ReadLines(p, next, 1000)
	if len(lines) != 1 || string(lines[0]) != "small" {
		t.Fatalf("%q", lines)
	}
}

func TestMaxBytesRespected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jsonl")
	write(t, p, "aaaa\nbbbb\ncccc\n")
	lines, next, _ := reader.ReadLines(p, 0, 10)
	if len(lines) != 2 || next != 10 {
		t.Fatalf("%q %d", lines, next)
	}
}

func TestHeadHash(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jsonl")
	write(t, p, "first")
	if _, ok, _ := reader.HeadHash(p); ok {
		t.Fatal("partial head accepted")
	}
	write(t, p, "first\nsecond\n")
	h, ok, _ := reader.HeadHash(p)
	if !ok || h != protocol.HashHex([]byte("first")) {
		t.Fatalf("%s %v", h, ok)
	}
}

func TestSnapshotAndFileID(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.json")
	write(t, p, `{"x":1}`)
	c, h, err := reader.Snapshot(p)
	if err != nil || string(c) != `{"x":1}` || h != protocol.HashHex(c) {
		t.Fatalf("%s %s %v", c, h, err)
	}
	id1, _ := reader.FileID(p)
	q := filepath.Join(dir, "b.json")
	write(t, q, "x")
	_ = os.Rename(q, p)
	id2, _ := reader.FileID(p)
	if id1 == "" || id1 == id2 {
		t.Fatalf("file id did not change on replace: %s %s", id1, id2)
	}
}

func TestFileDeletableWhileNotReading(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jsonl")
	write(t, p, "l\n")
	_, _, _ = reader.ReadLines(p, 0, 100)
	if err := os.Remove(p); err != nil {
		t.Fatalf("handle leaked: %v", err)
	}
}
