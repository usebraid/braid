// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"io/fs"
	"math/rand/v2"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/usebraid/braid/internal/daemon/auth"
	"github.com/usebraid/braid/internal/daemon/state"
	"github.com/usebraid/braid/internal/server/servertest"
)

func TestChaos(t *testing.T) {
	if os.Getenv("BRAID_E2E") != "1" {
		t.Skip("set BRAID_E2E=1 to run the chaos test (needs Postgres via BRAID_TEST_DATABASE_URL)")
	}
	env := servertest.Start(t)
	proxy := startProxy(t, env.URL) // returns *Proxy{URL string; Stop(); Start()}
	user := env.Login(t, "chaos")
	token, _ := env.NewDevice(t, user, "chaos-device")
	bin := buildBraid(t)
	dirs := newDaemonDirs(t) // ClaudeRoot, ConfigDir, DataDir
	writeFileToken(t, dirs.ConfigDir, proxy.URL, token)
	setServerURL(t, dirs.DataDir, proxy.URL) // opens state.DB, SetSetting server_url / token_store=file
	d := startDaemon(t, bin, dirs)           // *Daemon{Kill(); Restart()}
	s := seed(t)
	gen := NewGenerator(dirs.ClaudeRoot, rand.New(rand.NewPCG(s, 0)))
	ctx, stop := context.WithTimeout(context.Background(), duration())
	defer stop()
	go gen.Run(ctx)
	stats := chaos(ctx, t, d, proxy, gen, rand.New(rand.NewPCG(s, 1)))
	<-ctx.Done()
	stop()
	gen.Wait()
	if err := gen.Err(); err != nil {
		t.Fatalf("generator: %v", err)
	}
	proxy.Start()
	d.EnsureRunning()
	waitSynced(t, bin, dirs, 2*time.Minute) // polls `braid status` until exit 0 and "backlog: 0"
	got := loadServer(t, env.OwnerURL)      // map[key]map[offset]hash, snapshots set
	want := gen.Record()
	assertLines(t, want, got)
	assertSnapshots(t, want, got)
	assertSpoolEmpty(t, dirs.DataDir, want)
	if t.Failed() {
		d.DumpLog()
	}
	t.Logf("files=%d lines=%d kills=%d outages=%d deletions=%d replacements=%d", want.Files(), want.Lines(), stats.Kills, stats.Outages, stats.Deletions, stats.Replacements)
}

func seed(t testing.TB) uint64 {
	t.Helper()
	s := uint64(time.Now().UnixNano()) //nolint:gosec // time-based seed, logged below
	if v := os.Getenv("BRAID_E2E_SEED"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			t.Fatalf("BRAID_E2E_SEED: %v", err)
		}
		s = n
	}
	t.Logf("seed=%d (BRAID_E2E_SEED=%d reproduces the data; chaos timing still varies)", s, s)
	return s
}

func duration() time.Duration {
	if v := os.Getenv("BRAID_E2E_DURATION"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return 3 * time.Minute
}

// ---- proxy ----

// Proxy is a plaintext TCP listener on 127.0.0.1 that forwards every connection to the TLS test
// server. TLS ends here: the daemon accepts http only for loopback, and Go on macOS ignores
// SSL_CERT_FILE, so the daemon could not otherwise trust the test server's certificate.
type Proxy struct {
	URL string

	t        testing.TB
	upstream string
	tlsCfg   *tls.Config
	addr     string
	mu       sync.Mutex
	ln       net.Listener
	conns    map[net.Conn]struct{}
}

func startProxy(t testing.TB, serverURL string) *Proxy {
	t.Helper()
	u, err := url.Parse(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	// Pin the test server's self-signed certificate (loopback only) instead of skipping verification afterwards.
	c, err := tls.Dial("tcp", u.Host, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}) //nolint:gosec // fetch the test server's own certificate once, to pin it
	if err != nil {
		t.Fatal(err)
	}
	leaf := c.ConnectionState().PeerCertificates[0]
	_ = c.Close()
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	p := &Proxy{t: t, upstream: u.Host, tlsCfg: &tls.Config{RootCAs: pool, ServerName: u.Hostname(), MinVersion: tls.VersionTLS12}, conns: map[net.Conn]struct{}{}}
	p.addr = "127.0.0.1:0"
	p.Start()
	p.addr = p.ln.Addr().String() // restart on the same port
	p.URL = "http://" + p.addr
	t.Cleanup(p.Stop)
	return p
}

// Start begins accepting connections; no-op while running.
func (p *Proxy) Start() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ln != nil {
		return
	}
	var ln net.Listener
	var err error
	for range 50 { // the port may linger briefly after Stop
		if ln, err = net.Listen("tcp", p.addr); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		p.t.Errorf("proxy listen: %v", err)
		return
	}
	p.ln = ln
	go p.accept(ln)
}

// Stop refuses new connections and drops all open ones.
func (p *Proxy) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ln == nil {
		return
	}
	_ = p.ln.Close()
	p.ln = nil
	for c := range p.conns {
		_ = c.Close()
	}
}

func (p *Proxy) accept(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go p.forward(ln, c)
	}
}

func (p *Proxy) forward(ln net.Listener, c net.Conn) {
	up, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", p.upstream, p.tlsCfg)
	if err != nil {
		_ = c.Close()
		return
	}
	p.mu.Lock()
	if p.ln != ln { // stopped meanwhile
		p.mu.Unlock()
		_ = c.Close()
		_ = up.Close()
		return
	}
	p.conns[c], p.conns[up] = struct{}{}, struct{}{}
	p.mu.Unlock()
	var wg sync.WaitGroup
	cp := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		_ = dst.Close()
		_ = src.Close()
	}
	wg.Add(2)
	go cp(up, c)
	go cp(c, up)
	wg.Wait()
	p.mu.Lock()
	delete(p.conns, c)
	delete(p.conns, up)
	p.mu.Unlock()
}

// ---- daemon ----

type daemonDirs struct{ ClaudeRoot, ConfigDir, DataDir string }

func newDaemonDirs(t testing.TB) daemonDirs {
	t.Helper()
	root := t.TempDir()
	d := daemonDirs{filepath.Join(root, "claude"), filepath.Join(root, "config"), filepath.Join(root, "data")}
	for _, p := range []string{d.ClaudeRoot, d.ConfigDir, d.DataDir} {
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func buildBraid(t testing.TB) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "braid")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/braid") //nolint:gosec,noctx // fixed arguments
	cmd.Dir = ".."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func writeFileToken(t testing.TB, configDir, serverURL, token string) {
	t.Helper()
	if err := auth.NewFile(configDir).Set(serverURL, token); err != nil {
		t.Fatal(err)
	}
}

func setServerURL(t testing.TB, dataDir, serverURL string) {
	t.Helper()
	db, err := state.Open(filepath.Join(dataDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	for k, v := range map[string]string{"server_url": serverURL, "token_store": "file"} {
		if err := db.SetSetting(context.Background(), k, v); err != nil {
			t.Fatal(err)
		}
	}
}

func daemonEnv(dirs daemonDirs) []string {
	return append(os.Environ(), "CLAUDE_CONFIG_DIR="+dirs.ClaudeRoot, "BRAID_CONFIG_DIR="+dirs.ConfigDir, "BRAID_DATA_DIR="+dirs.DataDir)
}

// Daemon is the real braid binary running `braid run`.
type Daemon struct {
	t    testing.TB
	bin  string
	dirs daemonDirs
	log  string

	mu      sync.Mutex
	cmd     *exec.Cmd
	exited  chan struct{}
	killing bool
}

func startDaemon(t testing.TB, bin string, dirs daemonDirs) *Daemon {
	t.Helper()
	d := &Daemon{t: t, bin: bin, dirs: dirs, log: filepath.Join(filepath.Dir(dirs.DataDir), "daemon.log")}
	t.Cleanup(d.Kill)
	d.Restart()
	return d
}

// Restart starts the daemon if it is not running.
func (d *Daemon) Restart() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cmd != nil {
		return
	}
	lf, err := os.OpenFile(d.log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // test log in a temp dir
	if err != nil {
		d.t.Fatal(err)
	}
	cmd := exec.Command(d.bin, "run") //nolint:gosec,noctx // the binary we just built
	cmd.Env = daemonEnv(d.dirs)
	cmd.Stdout, cmd.Stderr = lf, lf
	if err := cmd.Start(); err != nil {
		d.t.Fatal(err)
	}
	_ = lf.Close()
	exited := make(chan struct{})
	d.cmd, d.exited, d.killing = cmd, exited, false
	go func() {
		err := cmd.Wait()
		d.mu.Lock()
		unexpected := !d.killing
		if d.cmd == cmd {
			d.cmd = nil
		}
		d.mu.Unlock()
		if unexpected {
			d.t.Errorf("daemon exited on its own: %v", err)
		}
		close(exited)
	}()
}

// Kill sends SIGKILL and waits for the process to be gone.
func (d *Daemon) Kill() {
	d.mu.Lock()
	cmd, exited := d.cmd, d.exited
	d.killing = true
	d.mu.Unlock()
	if cmd == nil {
		return
	}
	_ = cmd.Process.Kill()
	<-exited
}

// EnsureRunning restarts the daemon if it died.
func (d *Daemon) EnsureRunning() { d.Restart() }

// DumpLog prints the tail of the daemon's log.
func (d *Daemon) DumpLog() {
	b, _ := os.ReadFile(d.log)
	if len(b) > 8000 {
		b = b[len(b)-8000:]
	}
	d.t.Logf("daemon log tail:\n%s", b)
}

// status runs `braid status`.
func status(bin string, dirs daemonDirs) (code int, out string) {
	cmd := exec.Command(bin, "status") //nolint:gosec,noctx // the binary we just built
	cmd.Env = daemonEnv(dirs)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok { //nolint:errorlint // exec.ExitError is returned unwrapped
			return ee.ExitCode(), buf.String()
		}
		return -1, err.Error()
	}
	return 0, buf.String()
}

func synced(bin string, dirs daemonDirs) bool {
	code, out := status(bin, dirs)
	return code == 0 && strings.Contains(out, "backlog: 0 files")
}

// waitSynced polls `braid status` until it exits 0 with an empty backlog on three consecutive polls.
func waitSynced(t testing.TB, bin string, dirs daemonDirs, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	ok := 0
	var out string
	var code int
	for time.Now().Before(deadline) {
		if code, out = status(bin, dirs); code == 0 && strings.Contains(out, "backlog: 0 files") {
			if ok++; ok >= 3 {
				t.Logf("synced: %s", strings.ReplaceAll(strings.TrimSpace(out), "\n", " | "))
				return
			}
		} else {
			ok = 0
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("daemon not synced after %s: exit %d\n%s", timeout, code, out)
}

// ---- chaos ----

type chaosStats struct{ Kills, Outages, Deletions, Replacements int }

func chaos(ctx context.Context, t testing.TB, d *Daemon, proxy *Proxy, gen *Generator, rng *rand.Rand) chaosStats {
	var st chaosStats
	start := time.Now()
	total := duration()
	frac := func() float64 { return float64(time.Since(start)) / float64(total) }
	spool := filepath.Join(d.dirs.DataDir, "spool")
	between := func(lo, hi time.Duration) time.Duration { return lo + time.Duration(rng.Int64N(int64(hi-lo))) }

	// catchUp waits for the daemon to catch up (or 60 s), then lets snapshots change again.
	catchUp := func() {
		for end := time.Now().Add(60 * time.Second); time.Now().Before(end) && ctx.Err() == nil; time.Sleep(time.Second) {
			if synced(d.bin, d.dirs) {
				break
			}
		}
		gen.Hold(false)
	}
	kill := func(why string) {
		t.Logf("%5.0fs chaos: kill -9 daemon (%s)", time.Since(start).Seconds(), why)
		d.Kill()
		st.Kills++
	}
	// outage stops the proxy; act, if set, runs once the daemon had time to spool unacked data.
	outage := func(act func(), alsoKill bool) {
		dur := between(5*time.Second, 20*time.Second)
		t.Logf("%5.0fs chaos: server outage %s", time.Since(start).Seconds(), dur.Round(time.Second))
		gen.Hold(true)
		proxy.Stop()
		st.Outages++
		if act != nil {
			sleepCtx(ctx, 2*time.Second)
			act()
		}
		if alsoKill {
			sleepCtx(ctx, time.Second)
			kill("during outage")
			sleepCtx(ctx, between(0, 2*time.Second))
			d.Restart()
		}
		sleepCtx(ctx, dur)
		proxy.Start()
		catchUp()
	}
	// try retries for a few seconds until some file is eligible (spooled by the daemon).
	try := func(name string, f func(string) (string, bool)) (ok bool) {
		for range 20 {
			if rel, found := f(spool); found {
				t.Logf("%5.0fs chaos: %s %s", time.Since(start).Seconds(), name, rel)
				return true
			}
			if !sleepCtx(ctx, 500*time.Millisecond) {
				return false
			}
		}
		t.Logf("%5.0fs chaos: no spooled file to %s", time.Since(start).Seconds(), name)
		return false
	}
	for sleepCtx(ctx, between(5*time.Second, 15*time.Second)) {
		d.EnsureRunning()
		switch f := frac(); {
		case f >= 0.25 && st.Deletions < 1, f >= 0.55 && st.Deletions < 2, rng.IntN(8) == 0:
			outage(func() {
				if try("delete", gen.Delete) {
					st.Deletions++
				}
			}, rng.IntN(2) == 0)
		case f >= 0.4 && st.Replacements < 1:
			outage(func() {
				if try("replace", gen.Replace) {
					st.Replacements++
				}
			}, false)
		default:
			switch rng.IntN(4) {
			case 0:
				gen.Hold(true)
				kill("restart")
				sleepCtx(ctx, between(0, 2*time.Second))
				d.Restart()
				catchUp()
			case 1:
				outage(nil, false)
			default:
				t.Logf("%5.0fs chaos: nothing", time.Since(start).Seconds())
			}
		}
	}
	gen.Hold(false)
	return st
}

// ---- verification ----

type serverData struct {
	lines map[Key]map[int64][]string // rel/head -> offset -> content hashes of the stored rows
	snaps map[string]map[string]bool // rel -> content hashes
	dups  int                        // (source_file_id, offset, line_hash) groups with more than one row
	bad   int                        // rows whose line_hash differs from sha256(line)
}

func loadServer(t testing.TB, ownerURL string) *serverData {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	sd := &serverData{lines: map[Key]map[int64][]string{}, snaps: map[string]map[string]bool{}}
	rows, err := conn.Query(ctx, `SELECT f.rel_path, f.head_hash, r."offset", encode(sha256(r.line), 'hex'), r.line_hash = sha256(r.line)
	                                FROM raw_lines r JOIN source_files f ON f.id = r.source_file_id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var rel, head, h string
		var off int64
		var match bool
		if err := rows.Scan(&rel, &head, &off, &h, &match); err != nil {
			t.Fatal(err)
		}
		k := Key{rel, head}
		if sd.lines[k] == nil {
			sd.lines[k] = map[int64][]string{}
		}
		sd.lines[k][off] = append(sd.lines[k][off], h)
		if !match {
			sd.bad++
		}
	}
	rows.Close()
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	rows, err = conn.Query(ctx, `SELECT f.rel_path, encode(s.content_hash, 'hex') FROM raw_snapshots s JOIN source_files f ON f.id = s.source_file_id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var rel, h string
		if err := rows.Scan(&rel, &h); err != nil {
			t.Fatal(err)
		}
		if sd.snaps[rel] == nil {
			sd.snaps[rel] = map[string]bool{}
		}
		sd.snaps[rel][h] = true
	}
	rows.Close()
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM (SELECT 1 FROM raw_lines GROUP BY source_file_id, "offset", line_hash HAVING count(*) > 1) x`).Scan(&sd.dups); err != nil {
		t.Fatal(err)
	}
	return sd
}

func assertLines(t testing.TB, want *Record, got *serverData) {
	t.Helper()
	var problems []string
	add := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }
	total := 0
	for k, recs := range want.lines {
		g := got.lines[k]
		seen := map[int64]bool{}
		for _, r := range recs {
			seen[r.Offset] = true
			if hs := g[r.Offset]; len(hs) != 1 || hs[0] != r.Hash {
				total++
				add("%s head=%.8s offset=%d expected %.12s actual %v", k.Rel, k.Head, r.Offset, r.Hash, short(hs))
			}
		}
		for off, hs := range g {
			if !seen[off] {
				total++
				add("%s head=%.8s offset=%d never written, actual %v", k.Rel, k.Head, off, short(hs))
			}
		}
	}
	for k, g := range got.lines {
		if _, ok := want.lines[k]; !ok {
			total += len(g)
			add("%s head=%.8s: %d rows for a file generation the generator never wrote", k.Rel, k.Head, len(g))
		}
	}
	if got.dups != 0 {
		total++
		add("%d duplicate (source_file_id, offset, line_hash) groups", got.dups)
	}
	if got.bad != 0 {
		total++
		add("%d rows whose line_hash differs from sha256(line)", got.bad)
	}
	if total == 0 {
		return
	}
	slices.Sort(problems)
	if len(problems) > 10 {
		problems = problems[:10]
	}
	t.Errorf("%d line mismatches; first %d:\n  %s", total, len(problems), strings.Join(problems, "\n  "))
}

func short(hs []string) []string {
	out := make([]string, len(hs))
	for i, h := range hs {
		out[i] = h[:min(12, len(h))]
	}
	return out
}

func assertSnapshots(t testing.TB, want *Record, got *serverData) {
	t.Helper()
	missing := 0
	for rel, hs := range want.snaps {
		for _, h := range hs {
			if !got.snaps[rel][h] {
				if missing++; missing <= 10 {
					t.Errorf("snapshot %s version %.12s existed >= %s but is not stored", rel, h, snapshotMinLife)
				}
			}
		}
	}
	if missing > 10 {
		t.Errorf("%d snapshot versions missing in total", missing)
	}
}

func assertSpoolEmpty(t testing.TB, dataDir string, want *Record) {
	t.Helper()
	spool := filepath.Join(dataDir, "spool")
	_ = filepath.WalkDir(spool, func(p string, de fs.DirEntry, err error) error {
		if err == nil && !de.IsDir() {
			rel, _ := filepath.Rel(spool, p)
			t.Errorf("spool entry left behind (original gone: %v): %s", want.gone[filepath.ToSlash(rel)], rel)
		}
		return nil
	})
}
