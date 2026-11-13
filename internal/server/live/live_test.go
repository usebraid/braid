// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

//nolint:errcheck,noctx,bodyclose,gosec // test code: short-lived loopback requests, deferred best-effort closes
package live_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/usebraid/braid/internal/server"
	"github.com/usebraid/braid/internal/server/live"
	"github.com/usebraid/braid/internal/server/servertest"
	"github.com/usebraid/braid/internal/testdb"
)

func dial(t *testing.T, e *servertest.Env, c *http.Client, origin string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	h := http.Header{}
	if origin != "" {
		h.Set("Origin", origin)
	}
	return websocket.Dial(ctx, "wss"+strings.TrimPrefix(e.URL, "https")+"/api/v1/live", &websocket.DialOptions{HTTPClient: c, HTTPHeader: h})
}

func next(c *websocket.Conn, d time.Duration) (live.Message, error) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	for {
		_, b, err := c.Read(ctx)
		if err != nil {
			return live.Message{}, err
		}
		var m live.Message
		_ = json.Unmarshal(b, &m)
		if m.Type != "ping" && m.Type != "hello" {
			return m, nil
		}
	}
}

func uid(t *testing.T, e *servertest.Env, c *http.Client) string {
	t.Helper()
	r, err := c.Get(e.URL + "/api/v1/me")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var me struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&me)
	return me.ID
}

func notify(t *testing.T, e *servertest.Env, channel string, ev live.Event) {
	t.Helper()
	b, _ := json.Marshal(ev)
	testdb.Exec(t, e.OwnerURL, `SELECT pg_notify($1, $2)`, channel, string(b))
}

func TestRejectsAnonymousAndWrongOrigin(t *testing.T) {
	e := servertest.Start(t)
	if _, resp, err := dial(t, e, e.Client(), e.URL); err == nil || resp == nil || resp.StatusCode != 401 {
		t.Fatalf("anonymous: %v %v", resp, err)
	}
	a := e.Login(t, "alice")
	for _, origin := range []string{"", "https://evil.example", e.URL + ".evil.example"} {
		if _, resp, err := dial(t, e, a, origin); err == nil || resp == nil || resp.StatusCode != 403 {
			t.Fatalf("origin %q: %v %v", origin, resp, err)
		}
	}
	c, _, err := dial(t, e, a, e.URL)
	if err != nil {
		t.Fatalf("same origin refused: %v", err)
	}
	c.CloseNow()
}

func TestEventsReachOnlyTheirOwner(t *testing.T) {
	e := servertest.Start(t)
	a, b := e.Login(t, "alice"), e.Login(t, "bob")
	ca, _, err := dial(t, e, a, e.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer ca.CloseNow()
	cb, _, err := dial(t, e, b, e.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer cb.CloseNow()
	ua := uid(t, e, a)
	notify(t, e, live.ChannelDerived, live.Event{UserID: ua, Type: "session_updated", SessionID: "s1"})
	notify(t, e, live.ChannelSynced, live.Event{UserID: ua, Type: "device_synced", DeviceID: "d1"})
	notify(t, e, live.ChannelDerived, live.Event{UserID: ua, Type: "reindex", State: "done"})
	for _, want := range []live.Message{
		{Type: "session_updated", SessionID: "s1"}, {Type: "device_synced", DeviceID: "d1"}, {Type: "reindex", State: "done"},
	} {
		if m, err := next(ca, 5*time.Second); err != nil || m != want {
			t.Fatalf("alice: %+v %v, want %+v", m, err, want)
		}
	}
	if m, err := next(cb, 400*time.Millisecond); err == nil {
		t.Fatalf("bob received alice's event: %+v", m)
	}
}

func TestLogoutClosesTheSocket(t *testing.T) {
	e := servertest.Start(t, server.WithLive(live.Options{RecheckEvery: 100 * time.Millisecond}))
	a := e.Login(t, "alice")
	c, _, err := dial(t, e, a, e.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	req, _ := http.NewRequest("POST", e.URL+"/auth/logout", nil)
	req.Header.Set("Origin", e.URL)
	if r, err := a.Do(req); err != nil {
		t.Fatal(err)
	} else {
		r.Body.Close()
	}
	_, err = next(c, 5*time.Second)
	if websocket.CloseStatus(err) != 4401 {
		t.Fatalf("want close 4401, got %v", err)
	}
}

func TestPerUserConnectionCap(t *testing.T) {
	e := servertest.Start(t, server.WithLive(live.Options{MaxPerUser: 1}))
	a := e.Login(t, "alice")
	c, _, err := dial(t, e, a, e.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if _, resp, err := dial(t, e, a, e.URL); err == nil || resp == nil || resp.StatusCode != 429 {
		t.Fatalf("second connection: %v %v", resp, err)
	}
}

func TestClientFloodIsClosedWith4429(t *testing.T) {
	e := servertest.Start(t)
	c, _, err := dial(t, e, e.Login(t, "alice"), e.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		for range 40 {
			if c.Write(ctx, websocket.MessageText, []byte("x")) != nil {
				return
			}
		}
	}()
	if _, err := next(c, 5*time.Second); websocket.CloseStatus(err) != 4429 {
		t.Fatalf("want close 4429, got %v", err)
	}
}

func TestOversizedClientFrameIsRejected(t *testing.T) {
	e := servertest.Start(t)
	c, _, err := dial(t, e, e.Login(t, "alice"), e.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.Write(ctx, websocket.MessageText, []byte(strings.Repeat("x", 1024)))
	if _, err := next(c, 5*time.Second); websocket.CloseStatus(err) != websocket.StatusMessageTooBig {
		t.Fatalf("want close 1009, got %v", err)
	}
}

func TestRecheckDoesNotSlideTheSession(t *testing.T) {
	e := servertest.Start(t, server.WithLive(live.Options{RecheckEvery: 50 * time.Millisecond}))
	a := e.Login(t, "alice")
	testdb.Exec(t, e.OwnerURL, `UPDATE web_sessions SET expires_at = now() + interval '1 hour'`)
	c, _, err := dial(t, e, a, e.URL) // the upgrade request itself uses the side-effect free check
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	time.Sleep(400 * time.Millisecond)
	ctx := context.Background()
	pc, err := pgx.Connect(ctx, e.OwnerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pc.Close(ctx) }()
	var n int
	if err := pc.QueryRow(ctx, `SELECT count(*) FROM web_sessions WHERE expires_at < now() + interval '2 hours'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("session was extended: %d %v", n, err)
	}
}
