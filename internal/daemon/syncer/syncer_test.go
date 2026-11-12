// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package syncer_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/usebraid/braid/internal/daemon/client"
	"github.com/usebraid/braid/internal/daemon/spool"
	"github.com/usebraid/braid/internal/daemon/state"
	"github.com/usebraid/braid/internal/daemon/syncer"
	"github.com/usebraid/braid/internal/protocol"
)

type row struct {
	off  int64
	hash string
	text string
}

// fakeServer mimics the real server: gap check, dedupe by (path, head, offset, hash), snapshot dedupe.
type fakeServer struct {
	mu           sync.Mutex
	failNext     error
	dropResponse bool
	calls        int
	rows         map[[2]string][]row // (path, head) -> rows
	ends         map[[2]string]int64
	snaps        map[string][]string // path -> content hashes (append-only, newest last)
}

func newFakeServer() *fakeServer {
	return &fakeServer{rows: map[[2]string][]row{}, ends: map[[2]string]int64{}, snaps: map[string][]string{}}
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func (f *fakeServer) Ingest(_ context.Context, req protocol.IngestRequest) (protocol.IngestResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext != nil {
		err := f.failNext
		f.failNext = nil
		return protocol.IngestResponse{}, err
	}
	f.calls++
	var resp protocol.IngestResponse
	for _, fb := range req.Files {
		if fb.Kind == protocol.KindSnapshot {
			c, _ := fb.Content.Bytes()
			h := sum(c)
			if s := f.snaps[fb.Path]; len(s) == 0 || s[len(s)-1] != h {
				f.snaps[fb.Path] = append(s, h)
			}
			resp.Files = append(resp.Files, protocol.FileResult{Path: fb.Path, Status: protocol.StatusOK})
			continue
		}
		k := [2]string{fb.Path, fb.HeadHash}
		if fb.FromOffset > f.ends[k] {
			resp.Files = append(resp.Files, protocol.FileResult{Path: fb.Path, HeadHash: fb.HeadHash, Status: protocol.StatusGap, EndOffset: f.ends[k]})
			continue
		}
		off := fb.FromOffset
		for _, l := range fb.Lines {
			b, _ := l.Bytes()
			r := row{off, sum(b), string(b)}
			dup := false
			for _, e := range f.rows[k] {
				if e == r {
					dup = true
				}
			}
			if !dup {
				f.rows[k] = append(f.rows[k], r)
			}
			off += int64(len(b)) + 1
		}
		if off > f.ends[k] {
			f.ends[k] = off
		}
		resp.Files = append(resp.Files, protocol.FileResult{Path: fb.Path, HeadHash: fb.HeadHash, Status: protocol.StatusOK, EndOffset: off})
	}
	if f.dropResponse {
		return protocol.IngestResponse{}, &client.RetryableError{Status: 503}
	}
	return resp, nil
}

func (f *fakeServer) Files(context.Context) ([]protocol.RemoteFile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []protocol.RemoteFile
	for k, e := range f.ends {
		out = append(out, protocol.RemoteFile{Path: k[0], HeadHash: k[1], Kind: protocol.KindAppend, EndOffset: e})
	}
	for p, s := range f.snaps {
		out = append(out, protocol.RemoteFile{Path: p, Kind: protocol.KindSnapshot, ContentHash: s[len(s)-1]})
	}
	return out, nil
}

func (f *fakeServer) wipe() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows, f.ends, f.snaps = map[[2]string][]row{}, map[[2]string]int64{}, map[string][]string{}
}

func (f *fakeServer) lines(path string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var rs []row
	for k, v := range f.rows {
		if k[0] == path {
			rs = append(rs, v...)
		}
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].off < rs[j].off })
	var out []string
	for _, r := range rs {
		out = append(out, r.text)
	}
	return out
}

func (f *fakeServer) snapshots(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.snaps[path])
}
func (f *fakeServer) ingestCalls() int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }
func (f *fakeServer) rowCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, v := range f.rows {
		n += len(v)
	}
	return n
}
func (f *fakeServer) identities(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for k := range f.rows {
		if k[0] == path {
			n++
		}
	}
	return n
}

func newEngine(t *testing.T, fs *fakeServer) (*syncer.Engine, string) {
	t.Helper()
	root := t.TempDir()
	st, err := state.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &syncer.Engine{Root: root, State: st, Spool: spool.New(t.TempDir()), Up: fs, MaxBatchBytes: 64, MaxBatchLines: 3, Now: time.Now}, root
}

func appendTo(_ *testing.T, root, rel, s string) {
	p := filepath.Join(root, filepath.FromSlash(rel))
	_ = os.MkdirAll(filepath.Dir(p), 0o750)
	f, _ := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // test path under TempDir
	_, _ = f.WriteString(s)
	_ = f.Close()
}

func TestSyncAllAndIncremental(t *testing.T) {
	fs := newFakeServer()
	e, root := newEngine(t, fs)
	appendTo(t, root, "p/a.jsonl", "h\n1\n2\n3\n4\npartial")
	appendTo(t, root, "p/a/subagents/agent-x.meta.json", `{"t":"x"}`)
	if _, err := e.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fs.lines("p/a.jsonl"); strings.Join(got, ",") != "h,1,2,3,4" {
		t.Fatalf("%v", got)
	}
	if fs.snapshots("p/a/subagents/agent-x.meta.json") != 1 {
		t.Fatal("snapshot missing")
	}
	appendTo(t, root, "p/a.jsonl", "\n5\n")
	_, _ = e.Pass(context.Background())
	if got := fs.lines("p/a.jsonl"); strings.Join(got, ",") != "h,1,2,3,4,partial,5" {
		t.Fatalf("%v", got)
	}
	if fs.ingestCalls() == 0 {
		t.Fatal("no uploads")
	}
}

func TestLostAckIsIdempotent(t *testing.T) {
	fs := newFakeServer()
	e, root := newEngine(t, fs)
	appendTo(t, root, "p/a.jsonl", "h\n1\n")
	fs.dropResponse = true
	if _, err := e.Pass(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	fs.dropResponse = false
	if _, err := e.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fs.lines("p/a.jsonl"); strings.Join(got, ",") != "h,1" || fs.rowCount() != 2 {
		t.Fatalf("%v rows=%d", got, fs.rowCount())
	}
}

func TestGapRewinds(t *testing.T) {
	fs := newFakeServer()
	e, root := newEngine(t, fs)
	appendTo(t, root, "p/a.jsonl", "h\n1\n")
	_, _ = e.Pass(context.Background())
	fs.wipe() // server lost everything (e.g. restored from old backup)
	appendTo(t, root, "p/a.jsonl", "2\n")
	_, _ = e.Pass(context.Background())
	if got := fs.lines("p/a.jsonl"); strings.Join(got, ",") != "h,1,2" {
		t.Fatalf("%v", got)
	}
}

func TestOversizedLine(t *testing.T) {
	fs := newFakeServer()
	e, root := newEngine(t, fs)
	big := strings.Repeat("x", 500) // MaxBatchBytes is 64
	appendTo(t, root, "p/a.jsonl", "h\n"+big+"\nz\n")
	if _, err := e.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fs.lines("p/a.jsonl"); len(got) != 3 || got[1] != big {
		t.Fatalf("%d lines", len(got))
	}
}

func TestDeletedBeforeSyncReadFromSpool(t *testing.T) {
	fs := newFakeServer()
	e, root := newEngine(t, fs)
	appendTo(t, root, "p/a.jsonl", "h\n1\n")
	fs.failNext = &client.RetryableError{Status: 503}
	_, _ = e.Pass(context.Background()) // spools, upload fails
	_ = os.Remove(filepath.Join(root, "p/a.jsonl"))
	if _, err := e.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fs.lines("p/a.jsonl"); strings.Join(got, ",") != "h,1" {
		t.Fatalf("%v", got)
	}
	if entries, _ := os.ReadDir(e.Spool.Dir); len(entries) != 0 {
		t.Fatalf("spool not cleaned: %v", entries)
	}
}

func TestHeadChangeIsNewIdentity(t *testing.T) {
	fs := newFakeServer()
	e, root := newEngine(t, fs)
	appendTo(t, root, "p/a.jsonl", "h1\n1\n")
	_, _ = e.Pass(context.Background())
	_ = os.WriteFile(filepath.Join(root, "p/a.jsonl"), []byte("h2\n9\n"), 0o600)
	_, _ = e.Pass(context.Background())
	if fs.identities("p/a.jsonl") != 2 {
		t.Fatalf("identities %d", fs.identities("p/a.jsonl"))
	}
}

func TestResumeFromServerWhenStateLost(t *testing.T) {
	fs := newFakeServer()
	e, root := newEngine(t, fs)
	appendTo(t, root, "p/a.jsonl", "h\n1\n2\n")
	_, _ = e.Pass(context.Background())
	calls := fs.ingestCalls()
	e2, _ := newEngine(t, fs)
	e2.Root = root
	if _, err := e2.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fs.ingestCalls() != calls {
		t.Fatalf("resent %d requests after resume", fs.ingestCalls()-calls)
	}
	if fs.rowCount() != 3 {
		t.Fatalf("rows %d", fs.rowCount())
	}
}

func TestSnapshotOnlyOnChange(t *testing.T) {
	fs := newFakeServer()
	e, root := newEngine(t, fs)
	appendTo(t, root, "p/c.json", `{"a":1}`)
	_, _ = e.Pass(context.Background())
	_, _ = e.Pass(context.Background())
	if fs.ingestCalls() != 1 {
		t.Fatalf("calls %d", fs.ingestCalls())
	}
	_ = os.WriteFile(filepath.Join(root, "p/c.json"), []byte(`{"a":2}`), 0o600)
	_, _ = e.Pass(context.Background())
	if fs.snapshots("p/c.json") != 2 {
		t.Fatalf("versions %d", fs.snapshots("p/c.json"))
	}
}

func TestReplacedFileDrainsOldSpool(t *testing.T) {
	fs := newFakeServer()
	e, root := newEngine(t, fs)
	appendTo(t, root, "p/a.jsonl", "h\n")
	if _, err := e.Pass(context.Background()); err != nil { // acks the head line only
		t.Fatal(err)
	}
	appendTo(t, root, "p/a.jsonl", "1\n2\n")
	fs.failNext = &client.RetryableError{Status: 503}
	_, _ = e.Pass(context.Background()) // spools, upload fails
	tmp := filepath.Join(root, "p/new.tmp")
	_ = os.WriteFile(tmp, []byte("h\nn1\nn2\n"), 0o600)
	if err := os.Rename(tmp, filepath.Join(root, "p/a.jsonl")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, l := range fs.lines("p/a.jsonl") {
		got[l] = true
	}
	for _, w := range []string{"h", "1", "2", "n1", "n2"} {
		if !got[w] {
			t.Fatalf("missing %q in %v", w, got)
		}
	}
	if entries, _ := os.ReadDir(e.Spool.Dir); len(entries) != 0 {
		t.Fatalf("spool not cleaned: %v", entries)
	}
}

// A file replaced by one with a different head while the old one is unacked: the new file's spool
// entry must not overwrite the old one, and the old head's rows must hold exactly the old lines.
func TestReplacedWithNewHeadKeepsOldSpool(t *testing.T) {
	fs := newFakeServer()
	e, root := newEngine(t, fs)
	appendTo(t, root, "p/a.jsonl", "h1\n")
	if _, err := e.Pass(context.Background()); err != nil { // acks the head line only
		t.Fatal(err)
	}
	appendTo(t, root, "p/a.jsonl", "1\n2\n")
	fs.failNext = &client.RetryableError{Status: 503}
	_, _ = e.Pass(context.Background()) // spools the old file, upload fails
	tmp := filepath.Join(root, "p/new.tmp")
	_ = os.WriteFile(tmp, []byte("h2\nn1\nn2\nn3\n"), 0o600)
	if err := os.Rename(tmp, filepath.Join(root, "p/a.jsonl")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := map[[2]string]string{{"p/a.jsonl", sum([]byte("h1"))}: "h1,1,2", {"p/a.jsonl", sum([]byte("h2"))}: "h2,n1,n2,n3"}
	for k, w := range want {
		var got []string
		for _, r := range fs.rows[k] {
			got = append(got, r.text)
		}
		if strings.Join(got, ",") != w {
			t.Errorf("%s head %.6s: got %v, want %s", k[0], k[1], got, w)
		}
	}
	if entries, _ := os.ReadDir(e.Spool.Dir); len(entries) != 0 {
		t.Fatalf("spool not cleaned: %v", entries)
	}
}
