// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/usebraid/braid/internal/daemon/auth"
	"github.com/usebraid/braid/internal/daemon/cli"
	"github.com/usebraid/braid/internal/daemon/state"
	"github.com/usebraid/braid/internal/protocol"
)

const selfBody = `{"today":{"from":"2026-11-13T00:00:00Z","to":"2026-11-14T00:00:00Z","time_zone":"UTC","sessions":3,"agent_ms":125000,"cost_usd":4.25,"tokens":123456,"failures":2,"time_lost_s":0},
 "session":{"session_id":"s1","agent_ms":1,"total_agent_ms":1,"cost_usd":0.5,"tokens":900,"started_at":null,"ended_at":null,"failures":1},
 "limit":{"type":"five_hour","resets_at":"2026-11-13T15:00:00Z"}}`

const tok = "braid_dev_secret_token" //nolint:gosec // G101: test fixture

type statsRig struct {
	h    cli.Hooks
	srv  *httptest.Server
	hits *atomic.Int32
	now  *time.Time
	db   *state.DB
}

func newRig(t *testing.T, handler http.HandlerFunc) *statsRig {
	t.Helper()
	r := &statsRig{hits: new(atomic.Int32)}
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	r.now = &now
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		r.hits.Add(1)
		if q.Header.Get("Authorization") != "Bearer "+tok || q.URL.Path != "/v1/stats/self" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		handler(w, q)
	}))
	t.Cleanup(r.srv.Close)
	r.h = cli.Hooks{ConfigDir: t.TempDir(), DataDir: t.TempDir(), Now: func() time.Time { return *r.now }}
	db, err := state.Open(filepath.Join(r.h.DataDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	r.db = db
	ctx := context.Background()
	_ = db.SetSetting(ctx, "server_url", r.srv.URL)
	_ = db.SetSetting(ctx, "token_store", "file")
	if err := auth.NewFile(r.h.ConfigDir).Set(r.srv.URL, tok); err != nil {
		t.Fatal(err)
	}
	return r
}

func okHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(selfBody))
}

func (r *statsRig) stats(t *testing.T, args ...string) (cli.StatsOut, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	code := cli.RunWithHooks(context.Background(), append([]string{"stats"}, args...), nil, &out, &errb, r.h)
	if strings.Contains(out.String()+errb.String(), tok) {
		t.Fatal("token leaked")
	}
	var s cli.StatsOut
	if code == 0 {
		if err := json.Unmarshal(out.Bytes(), &s); err != nil || strings.Count(out.String(), "\n") != 1 {
			t.Fatalf("not one JSON line: %q %v", out.String(), err)
		}
	}
	return s, out.String(), code
}

func TestStatsOKWithSessionAndLimit(t *testing.T) {
	r := newRig(t, okHandler)
	s, raw, code := r.stats(t, "--json", "--session", "s1")
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	want := cli.StatsOut{Version: 1, Sync: cli.StatsSync{State: "ok"},
		Today:   cli.StatsToday{CostUSD: 4.25, Tokens: 123456, Sessions: 3, AgentTimeS: 125, Failures: 2},
		Session: &cli.StatsSession{CostUSD: 0.5, Tokens: 900, Failures: 1},
		Limit:   &cli.StatsLimit{Type: "five_hour", ResetsAt: "2026-11-13T15:00:00Z"}}
	if s.Session == nil || s.Limit == nil || *s.Session != *want.Session || *s.Limit != *want.Limit || s.Version != 1 || s.Sync != want.Sync || s.Today != want.Today {
		t.Fatalf("got %s", raw)
	}
}

func TestStatsOmitsSessionAndLimitWhenUnknown(t *testing.T) {
	r := newRig(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"today":{"sessions":1,"agent_ms":2000,"cost_usd":1,"tokens":10,"failures":0},"session":null,"limit":null}`))
	})
	_, raw, _ := r.stats(t, "--json", "--session", "ghost")
	if strings.Contains(raw, `"session"`) || strings.Contains(raw, `"limit"`) || !strings.Contains(raw, `"today"`) {
		t.Fatalf("%s", raw)
	}
	if strings.Contains(raw, `"last_ack"`) {
		t.Fatalf("last_ack must be omitted when never acked: %s", raw)
	}
}

func TestStatsCacheThirtySeconds(t *testing.T) {
	r := newRig(t, okHandler)
	r.stats(t, "--json", "--session", "s1")
	*r.now = r.now.Add(29 * time.Second)
	r.stats(t, "--json", "--session", "s1")
	if r.hits.Load() != 1 {
		t.Fatalf("hits %d, want 1 (cache)", r.hits.Load())
	}
	r.stats(t, "--json", "--session", "other") // other session: refetch
	if r.hits.Load() != 2 {
		t.Fatalf("hits %d, want 2", r.hits.Load())
	}
	*r.now = r.now.Add(31 * time.Second)
	r.stats(t, "--json", "--session", "other")
	if r.hits.Load() != 3 {
		t.Fatalf("hits %d, want 3 (expired)", r.hits.Load())
	}
	fi, err := os.Stat(filepath.Join(r.h.DataDir, "stats-cache.json"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("cache file %v %v", fi, err)
	}
	b, _ := os.ReadFile(filepath.Join(r.h.DataDir, "stats-cache.json"))
	if strings.Contains(string(b), tok) {
		t.Fatal("token in cache")
	}
}

func TestStatsLocalStateMapping(t *testing.T) {
	r := newRig(t, okHandler)
	ctx := context.Background()
	if s, _, _ := r.stats(t, "--json"); s.Sync.State != "ok" {
		t.Fatalf("ok: %+v", s.Sync)
	}
	ack := time.Date(2026, 11, 13, 11, 0, 0, 0, time.UTC)
	_ = r.db.Put(ctx, state.File{RelPath: "p/a.jsonl", HeadHash: "h", Kind: protocol.KindAppend, SizeSeen: 100, AckedOffset: 40, LastAckAt: ack})
	s, _, _ := r.stats(t, "--json")
	if s.Sync.State != "backlog" || s.Sync.BacklogBytes != 60 || s.Sync.LastAck != "2026-11-13T11:00:00Z" {
		t.Fatalf("backlog: %+v", s.Sync)
	}
	_ = r.db.SetSetting(ctx, "blocked", "1 file(s) cannot be synced")
	if s, _, _ := r.stats(t, "--json"); s.Sync.State != "blocked" {
		t.Fatalf("blocked beats backlog: %+v", s.Sync)
	}
}

func TestStatsOfflineUsesStaleCacheThenZeros(t *testing.T) {
	r := newRig(t, okHandler)
	r.stats(t, "--json", "--session", "s1")
	r.srv.Close()
	*r.now = r.now.Add(10 * time.Minute)
	s, _, code := r.stats(t, "--json", "--session", "s1")
	if code != 0 || s.Sync.State != "offline" || s.Today.Tokens != 123456 || s.Session == nil {
		t.Fatalf("stale: %d %+v", code, s)
	}
	s, _, _ = r.stats(t, "--json", "--session", "different")
	if s.Sync.State != "offline" || s.Session != nil || s.Today.Tokens != 123456 {
		t.Fatalf("other session offline: %+v", s)
	}
	_ = os.Remove(filepath.Join(r.h.DataDir, "stats-cache.json"))
	s, _, _ = r.stats(t, "--json")
	if s.Sync.State != "offline" || s.Today != (cli.StatsToday{}) {
		t.Fatalf("no cache: %+v", s)
	}
}

func TestStatsLoggedOut(t *testing.T) {
	// never logged in
	h := cli.Hooks{ConfigDir: t.TempDir(), DataDir: t.TempDir()}
	var out, errb bytes.Buffer
	if code := cli.RunWithHooks(context.Background(), []string{"stats", "--json"}, nil, &out, &errb, h); code != 0 || !strings.Contains(out.String(), `"state":"logged_out"`) {
		t.Fatalf("%d %s %s", code, out.String(), errb.String())
	}
	// token rejected by the server: logged_out and the cache is dropped
	r := newRig(t, okHandler)
	r.stats(t, "--json")
	_ = auth.NewFile(r.h.ConfigDir).Set(r.srv.URL, "braid_dev_revoked") //nolint:gosec // G101: test fixture
	*r.now = r.now.Add(time.Minute)
	s, _, _ := r.stats(t, "--json")
	if s.Sync.State != "logged_out" || s.Today != (cli.StatsToday{}) {
		t.Fatalf("revoked: %+v", s)
	}
	if _, err := os.Stat(filepath.Join(r.h.DataDir, "stats-cache.json")); !os.IsNotExist(err) {
		t.Fatalf("cache kept after 401: %v", err)
	}
}

func TestStatsUsageErrors(t *testing.T) {
	r := newRig(t, okHandler)
	for _, args := range [][]string{{}, {"--session", "x"}, {"--json", "--session"}, {"--json", "--bogus"}, {"--json", "extra"}} {
		if _, _, code := r.stats(t, args...); code != 2 {
			t.Fatalf("%v: code %d", args, code)
		}
	}
	if r.hits.Load() != 0 {
		t.Fatal("usage errors must not hit the network")
	}
}
