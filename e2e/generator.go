// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

//go:build e2e

// Package e2e holds the chaos end-to-end test: real server, real daemon binary, random faults.
package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Key identifies one file generation as the server does: path plus hash of its first line.
type Key struct{ Rel, Head string }

// LineRec is one line the generator wrote: where it starts and the hash of its bytes (without '\n').
type LineRec struct {
	Offset int64
	Hash   string
}

// Record is everything the generator wrote that the server must hold afterwards.
type Record struct {
	lines map[Key][]LineRec
	snaps map[string][]string // rel path -> content hashes of versions that existed >= snapshotMinLife (and the final one)
	gone  map[string]bool     // rel paths of originals the generator deleted
}

// Files returns the number of recorded file generations.
func (r *Record) Files() int { return len(r.lines) }

// Lines returns the number of recorded lines.
func (r *Record) Lines() (n int) {
	for _, l := range r.lines {
		n += len(l)
	}
	return n
}

const snapshotMinLife = 2 * time.Second

func hashHex(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// Generator imitates Claude Code: N sessions, each with a main transcript and 0-2 subagent transcripts
// (.jsonl, append only) plus a .meta.json per subagent (rewritten whole).
type Generator struct {
	root     string // CLAUDE_CONFIG_DIR; files live below <root>/projects
	rng      *rand.Rand
	hold     atomic.Bool
	done     chan struct{}
	mu       sync.Mutex
	appends  []*appendFile
	snaps    []*snapFile
	firstErr error
}

// NewGenerator returns a generator writing below root/projects.
func NewGenerator(root string, rng *rand.Rand) *Generator {
	return &Generator{root: root, rng: rng, done: make(chan struct{})}
}

// Hold pauses (true) or resumes (false) the rewriting of .meta.json files. A snapshot is only
// synced as its newest content, so versions would be lost to any outage that outlasts a rewrite;
// the chaos loop holds them while the sync path is faulty and until the daemon caught up again.
func (g *Generator) Hold(on bool) { g.hold.Store(on) }

// Err returns the first write error, if any.
func (g *Generator) Err() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.firstErr
}

func (g *Generator) fail(err error) {
	g.mu.Lock()
	if g.firstErr == nil {
		g.firstErr = err
	}
	g.mu.Unlock()
}

// Wait blocks until Run has returned.
func (g *Generator) Wait() { <-g.done }

func (g *Generator) child() *rand.Rand {
	g.mu.Lock()
	defer g.mu.Unlock()
	return rand.New(rand.NewPCG(g.rng.Uint64(), g.rng.Uint64()))
}

// Run starts six sessions and blocks until ctx ends and every writer has stopped.
func (g *Generator) Run(ctx context.Context) {
	defer close(g.done)
	var wg sync.WaitGroup
	for i := range 6 {
		wg.Add(1)
		go func() { defer wg.Done(); g.session(ctx, i) }()
	}
	wg.Wait()
	for _, s := range g.snaps { // the final version of a surviving file must be stored, however short it lived
		s.finish()
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (g *Generator) session(ctx context.Context, n int) {
	rng := g.child()
	proj := fmt.Sprintf("-tmp-chaos-proj%d", n)
	sid := fmt.Sprintf("%016x", rng.Uint64())
	var wg sync.WaitGroup
	start := func(rel string, meta bool) {
		f := g.newAppend(rel)
		wg.Add(1)
		go func() { defer wg.Done(); f.loop(ctx, g) }()
		if meta {
			s := g.newSnap(strings.TrimSuffix(rel, ".jsonl") + ".meta.json")
			wg.Add(1)
			go func() { defer wg.Done(); s.loop(ctx, g) }()
		}
	}
	start(proj+"/"+sid+".jsonl", false)
	var after time.Duration
	if dl, ok := ctx.Deadline(); ok {
		after = time.Until(dl) / 2
	} else {
		after = time.Minute
	}
	for range rng.IntN(3) {
		if !sleepCtx(ctx, time.Duration(rng.Int64N(int64(after)+1))) {
			break
		}
		start(fmt.Sprintf("%s/%s/subagents/agent-%08x.jsonl", proj, sid, rng.Uint32()), true)
	}
	wg.Wait()
}

// ---- append files ----

type appendFile struct {
	g        *Generator
	rng      *rand.Rand
	rel, abs string
	mu       sync.Mutex
	f        *os.File
	head     string
	off      int64
	n        int
	gone     bool
	recs     map[Key][]LineRec
}

func (g *Generator) newAppend(rel string) *appendFile {
	a := &appendFile{g: g, rng: g.child(), rel: rel, abs: filepath.Join(g.root, "projects", filepath.FromSlash(rel)), recs: map[Key][]LineRec{}}
	g.mu.Lock()
	g.appends = append(g.appends, a)
	g.mu.Unlock()
	return a
}

func (a *appendFile) open() error {
	if err := os.MkdirAll(filepath.Dir(a.abs), 0o750); err != nil {
		return err
	}
	f, err := os.OpenFile(a.abs, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // test file under a temp dir
	if err != nil {
		return err
	}
	a.f, a.head, a.off, a.n = f, "", 0, 0
	return nil
}

func (a *appendFile) loop(ctx context.Context, g *Generator) {
	a.mu.Lock()
	err := a.open()
	a.mu.Unlock()
	if err != nil {
		g.fail(err)
		return
	}
	for sleepCtx(ctx, time.Duration(a.rng.Int64N(int64(50*time.Millisecond)))) {
		if !a.appendLine() {
			return
		}
	}
	a.mu.Lock()
	if a.f != nil {
		_ = a.f.Close()
	}
	a.mu.Unlock()
}

// appendLine writes one line, sometimes in two halves. The lock is held across both, so a delete
// or replace never cuts a line in half. Returns false once the file is gone or broken.
func (a *appendFile) appendLine() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.gone {
		return false
	}
	b := a.makeLine()
	buf := append(b, '\n') //nolint:gocritic // b is ours
	var err error
	if len(buf) > 4 && a.rng.IntN(5) == 0 {
		h := 1 + a.rng.IntN(len(buf)-1)
		if _, err = a.f.Write(buf[:h]); err == nil {
			time.Sleep(time.Duration(1+a.rng.IntN(5)) * time.Millisecond)
			_, err = a.f.Write(buf[h:])
		}
	} else {
		_, err = a.f.Write(buf)
	}
	if err != nil {
		a.g.fail(err)
		return false
	}
	if a.head == "" {
		a.head = hashHex(b)
	}
	k := Key{a.rel, a.head}
	a.recs[k] = append(a.recs[k], LineRec{Offset: a.off, Hash: hashHex(b)})
	a.off += int64(len(buf))
	a.n++
	return true
}

const pad = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 _-.,:;äöü✓"

func lineSize(r *rand.Rand) int {
	switch p := r.Float64(); {
	case p < 0.90:
		return 10 + r.IntN(2000)
	case p < 0.99:
		return 2000 + r.IntN(30000)
	default:
		return 30000 + r.IntN(270001)
	}
}

// makeLine returns a line without '\n': mostly JSON-looking valid UTF-8, sometimes raw bytes with invalid UTF-8.
// Every line carries a random nonce, so no two lines (and no two first lines) are equal.
func (a *appendFile) makeLine() []byte {
	size := lineSize(a.rng)
	if a.rng.IntN(100) < 12 {
		b := make([]byte, max(size, 10))
		for i := 0; i < len(b); i += 8 {
			v := a.rng.Uint64()
			for j := 0; j < 8 && i+j < len(b); j++ {
				b[i+j] = byte(v >> (8 * j))
			}
		}
		for i := range b {
			if b[i] == '\n' {
				b[i] = 0x0b
			}
		}
		b[a.rng.IntN(len(b))] = 0xff // never valid UTF-8
		return b
	}
	b := fmt.Appendf(nil, `{"n":%d,"r":"%016x","pad":"`, a.n, a.rng.Uint64())
	pr := []rune(pad)
	for len(b) < size-2 {
		b = append(b, string(pr[a.rng.IntN(len(pr))])...)
	}
	return append(b, '"', '}')
}

// eligible reports whether the daemon holds a spool link to the file's current inode, i.e. all
// bytes written so far survive the original's deletion or replacement.
func (a *appendFile) eligible(spoolDir string) bool {
	if a.gone || a.head == "" {
		return false
	}
	cur, err1 := os.Stat(a.abs)
	sp, err2 := os.Stat(filepath.Join(spoolDir, filepath.FromSlash(a.rel)))
	return err1 == nil && err2 == nil && os.SameFile(cur, sp)
}

func (g *Generator) pick(spoolDir string, act func(*appendFile) error) (string, bool) {
	g.mu.Lock()
	c := append([]*appendFile(nil), g.appends...)
	r := g.rng
	r.Shuffle(len(c), func(i, j int) { c[i], c[j] = c[j], c[i] })
	g.mu.Unlock()
	for _, a := range c {
		a.mu.Lock()
		if a.f != nil && a.eligible(spoolDir) {
			err := act(a)
			a.mu.Unlock()
			if err != nil {
				g.fail(err)
				return "", false
			}
			return a.rel, true
		}
		a.mu.Unlock()
	}
	return "", false
}

// Delete removes the original of one file the daemon already spooled (never one it has not seen).
func (g *Generator) Delete(spoolDir string) (string, bool) {
	return g.pick(spoolDir, func(a *appendFile) error {
		_ = a.f.Close()
		a.gone = true
		return os.Remove(a.abs)
	})
}

// Replace swaps one spooled file for new content with a new first line (new head) under the same path.
func (g *Generator) Replace(spoolDir string) (string, bool) {
	return g.pick(spoolDir, func(a *appendFile) error {
		tmp := filepath.Join(filepath.Dir(a.abs), ".replace-tmp")
		if err := os.WriteFile(tmp, nil, 0o600); err != nil {
			return err
		}
		if err := os.Rename(tmp, a.abs); err != nil { // the old inode lives on in the spool only
			return err
		}
		_ = a.f.Close()
		return a.open()
	})
}

// ---- snapshot files ----

type snapFile struct {
	g       *Generator
	rng     *rand.Rand
	abs     string
	rel     string
	n       int
	cur     string
	curAt   time.Time
	durable []string
}

func (g *Generator) newSnap(rel string) *snapFile {
	s := &snapFile{g: g, rng: g.child(), rel: rel, abs: filepath.Join(g.root, "projects", filepath.FromSlash(rel))}
	g.mu.Lock()
	g.snaps = append(g.snaps, s)
	g.mu.Unlock()
	return s
}

func (s *snapFile) write() error {
	if s.cur != "" && time.Since(s.curAt) >= snapshotMinLife {
		s.durable = append(s.durable, s.cur)
	}
	s.n++
	b := fmt.Appendf(nil, `{"v":%d,"r":"%016x","note":"`, s.n, s.rng.Uint64())
	for range 20 + s.rng.IntN(2000) {
		b = append(b, 'a'+byte(s.rng.IntN(26)))
	}
	b = append(b, '"', '}')
	if err := os.MkdirAll(filepath.Dir(s.abs), 0o750); err != nil {
		return err
	}
	tmp := s.abs[:len(s.abs)-len(filepath.Base(s.abs))] + ".tmp-" + filepath.Base(s.abs) // dot-prefixed: ignored by the daemon
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.abs); err != nil {
		return err
	}
	s.cur, s.curAt = hashHex(b), time.Now()
	return nil
}

func (s *snapFile) loop(ctx context.Context, g *Generator) {
	if err := s.write(); err != nil {
		g.fail(err)
		return
	}
	for sleepCtx(ctx, time.Second+time.Duration(s.rng.Int64N(int64(5*time.Second)))) {
		for g.hold.Load() {
			if !sleepCtx(ctx, 100*time.Millisecond) {
				return
			}
		}
		if err := s.write(); err != nil {
			g.fail(err)
			return
		}
	}
}

func (s *snapFile) finish() {
	if s.cur != "" {
		s.durable = append(s.durable, s.cur)
		s.cur = ""
	}
}

// Record returns what was written. Call it after Wait.
func (g *Generator) Record() *Record {
	r := &Record{lines: map[Key][]LineRec{}, snaps: map[string][]string{}, gone: map[string]bool{}}
	for _, a := range g.appends {
		for k, v := range a.recs {
			r.lines[k] = append(r.lines[k], v...)
		}
		if a.gone {
			r.gone[a.rel] = true
		}
	}
	for _, s := range g.snaps {
		r.snaps[s.rel] = append(r.snaps[s.rel], s.durable...)
	}
	return r
}
