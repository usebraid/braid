// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

//go:build webe2e

// Package web is the server harness for the Playwright specs in web/e2e: a real braid-server with
// embedded SPA, worker and WebSocket hub, a mock IdP and fixture users. Run it via `playwright test`.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/usebraid/braid/internal/server"
	"github.com/usebraid/braid/internal/server/config"
	dt "github.com/usebraid/braid/internal/server/derive/derivetest"
	"github.com/usebraid/braid/internal/server/webauth/oidctest"
	"github.com/usebraid/braid/internal/testdb"
)

const (
	publicAddr  = "localhost:4173"
	controlAddr = "localhost:4174"
	opus        = "claude-opus-5-5"
)

// jarless keeps cookies by hand: the session cookies are Secure and net/http's jar withholds those from http://.
type jarless struct {
	base    http.RoundTripper
	mu      sync.Mutex
	cookies map[string]string
}

func (j *jarless) RoundTrip(r *http.Request) (*http.Response, error) {
	j.mu.Lock()
	if r.URL.Host == publicAddr {
		for k, v := range j.cookies {
			r.AddCookie(&http.Cookie{Name: k, Value: v}) //nolint:gosec // outgoing request cookie; attributes only matter on Set-Cookie
		}
	}
	j.mu.Unlock()
	resp, err := j.base.RoundTrip(r)
	if err == nil && r.URL.Host == publicAddr {
		j.mu.Lock()
		for _, c := range resp.Cookies() {
			if c.MaxAge < 0 {
				delete(j.cookies, c.Name)
			} else {
				j.cookies[c.Name] = c.Value
			}
		}
		j.mu.Unlock()
	}
	return resp, err
}

// login runs the real /auth/login flow as the IdP user and returns the user id from /api/v1/me.
func login(t *testing.T, idp *oidctest.Provider, u oidctest.User) string {
	t.Helper()
	idp.SetUser(u)
	c := &http.Client{Transport: &jarless{base: http.DefaultTransport, cookies: map[string]string{}}, Timeout: 10 * time.Second}
	get := func(path string) *http.Response {
		resp, err := c.Get("http://" + publicAddr + path) //nolint:noctx // test harness
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	_ = get("/auth/login").Body.Close()
	resp := get("/api/v1/me")
	defer func() { _ = resp.Body.Close() }()
	var me struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil || me.ID == "" {
		t.Fatalf("login as %s: status %d, %v", u.Sub, resp.StatusCode, err)
	}
	return me.ID
}

func stamp(ago time.Duration) string {
	return time.Now().Add(-ago).UTC().Format("2006-01-02T15:04:05.000Z")
}

// TestServe runs the harness until SIGTERM/SIGINT.
func TestServe(t *testing.T) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	ownerURL, appURL := testdb.New(t)
	idp := oidctest.New(t, "braid", "secret")
	pub, _ := url.Parse("http://" + publicAddr)
	s, err := server.New(ctx, config.Config{PublicURL: pub, DatabaseURL: appURL, DatabaseOwnerURL: ownerURL,
		OIDCIssuer: idp.Issuer, OIDCClientID: "braid", OIDCClientSecret: "secret",
		OIDCScopes: []string{"openid", "profile", "email", "groups"}, OIDCGroupsClaim: "groups", OIDCAdminGroup: "braid-admins"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	go func() { _ = s.RunWorker(ctx) }()

	var ready atomic.Bool
	h := s.Handler()
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" && !ready.Load() { // Playwright starts once the fixtures are in
			http.Error(w, "seeding", http.StatusServiceUnavailable)
			return
		}
		h.ServeHTTP(w, r)
	}), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second} // as cmd/braid-server
	ln, err := net.Listen("tcp", publicAddr)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()

	aliceID := login(t, idp, oidctest.User{Sub: "alice", Email: "alice@example.test", Name: "alice", Groups: []string{"braid-admins"}})
	bobID := login(t, idp, oidctest.User{Sub: "bob", Email: "bob@example.test", Name: "bob"})

	env := dt.Wrap(t, ownerURL, appURL)
	laptop, desktop := env.Device(t, aliceID, "laptop"), env.Device(t, aliceID, "desktop")
	env.Put(t, aliceID, laptop, dt.Scenario()...)
	env.Put(t, aliceID, laptop, bigSession()...)
	beta := func(l string) string { return dt.Cwd(l, "/work/beta") }
	env.Put(t, aliceID, desktop, dt.File{Rel: "-work-beta/b1.jsonl", Lines: []string{
		beta(dt.Prompt(dt.At(10), "Beta prompt")),
		beta(dt.Reply(dt.At(20), "b-m1", opus, dt.Usage(100, 10, 0, 0, 0), dt.Text("beta reply"))),
		beta(dt.Line("ai-title", "", "aiTitle", "Beta small")),
	}})
	live := func(l string) string { return dt.Cwd(l, "/work/live") }
	start := stamp(0)
	env.Put(t, aliceID, laptop, dt.File{Rel: "-work-live/l1.jsonl", Lines: []string{
		live(dt.Prompt(start, "Live fixture")),
		live(dt.Reply(start, "l-m1", opus, dt.Usage(100, 10, 0, 0, 0), dt.Tool("lt1", "Bash", map[string]any{"command": "sleep 60"}))), // no result: running tool
	}})
	env.Put(t, bobID, env.Device(t, bobID, "bobbox"), dt.File{Rel: "-work-bob/x1.jsonl", Lines: []string{
		dt.Cwd(dt.Prompt(dt.At(10), "Bob prompt"), "/work/bob"),
		dt.Cwd(dt.Line("ai-title", "", "aiTitle", "Bob private"), "/work/bob"),
	}})
	env.Derive(t, aliceID, 0)
	env.Derive(t, bobID, 0)

	var appendMu sync.Mutex
	appended := 0
	ctl := http.NewServeMux()
	ctl.HandleFunc("POST /user", func(w http.ResponseWriter, r *http.Request) {
		sub := r.URL.Query().Get("sub")
		u := oidctest.User{Sub: sub, Email: sub + "@example.test", Name: sub}
		if r.URL.Query().Get("admin") == "1" {
			u.Groups = []string{"braid-admins"}
		}
		idp.SetUser(u)
		w.WriteHeader(http.StatusNoContent)
	})
	ctl.HandleFunc("POST /append", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != "l1" {
			http.Error(w, "only l1", http.StatusBadRequest)
			return
		}
		appendMu.Lock()
		defer appendMu.Unlock()
		appended++ // shortcut: Put/N fail the test via t.Fatal on this goroutine; fine for a harness, upgrade if control calls must report errors
		env.Put(t, aliceID, laptop, dt.File{Rel: "-work-live/l1.jsonl", Lines: []string{
			live(dt.Reply(stamp(0), fmt.Sprintf("l-m%d", appended+1), opus, dt.Usage(10, 10, 0, 0, 0), dt.Text("live reply"))),
		}})
		env.N(t, `SELECT 1 FROM (SELECT pg_notify('braid_ingest', $1)) x`, aliceID)
		w.WriteHeader(http.StatusNoContent)
	})
	cln, err := net.Listen("tcp", controlAddr)
	if err != nil {
		t.Fatal(err)
	}
	csrv := &http.Server{Handler: ctl, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = csrv.Serve(cln) }()

	ready.Store(true)
	<-ctx.Done()
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = csrv.Shutdown(sctx)
	_ = srv.Shutdown(sctx)
}
