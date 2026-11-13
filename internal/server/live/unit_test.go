// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

//nolint:errcheck,bodyclose // test code: deferred best-effort close
package live

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/usebraid/braid/internal/server/webauth"
)

func fakeListen(ctx context.Context, _ string, ready func(), _ func(string)) error {
	ready()
	<-ctx.Done()
	return nil
}

func TestConnectionSurvivesServerWriteTimeout(t *testing.T) {
	h := NewHub(fakeListen, Options{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = h.Run(ctx) }()
	<-h.Ready()
	srv := httptest.NewUnstartedServer(nil)
	pub, _ := url.Parse("http://" + srv.Listener.Addr().String())
	srv.Config.Handler = h.Handler(pub, func(*http.Request) (webauth.Principal, bool, error) {
		return webauth.Principal{UserID: "u1"}, true, nil
	})
	srv.Config.WriteTimeout = 300 * time.Millisecond
	srv.Start()
	defer srv.Close()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {pub.String()}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	time.Sleep(900 * time.Millisecond) // three times the server's WriteTimeout
	h.Publish("u1", Message{Type: "resync"})
	rctx, rc := context.WithTimeout(ctx, 3*time.Second)
	defer rc()
	for {
		_, b, err := c.Read(rctx)
		if err != nil {
			t.Fatalf("connection died: %v", err)
		}
		if strings.Contains(string(b), "resync") {
			return
		}
	}
}

func TestRecheckKeepsTheSocketOnDatabaseErrors(t *testing.T) {
	h := NewHub(fakeListen, Options{RecheckEvery: 30 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = h.Run(ctx) }()
	<-h.Ready()
	var calls atomic.Int32
	check := func(*http.Request) (webauth.Principal, bool, error) {
		switch n := calls.Add(1); {
		case n == 1:
			return webauth.Principal{UserID: "u1"}, true, nil
		case n <= 4:
			return webauth.Principal{}, false, errors.New("db down")
		default:
			return webauth.Principal{}, false, nil // really expired
		}
	}
	srv := httptest.NewUnstartedServer(nil)
	pub, _ := url.Parse("http://" + srv.Listener.Addr().String())
	srv.Config.Handler = h.Handler(pub, check)
	srv.Start()
	defer srv.Close()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {pub.String()}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	rctx, rc := context.WithTimeout(ctx, 3*time.Second)
	defer rc()
	for {
		_, _, err := c.Read(rctx)
		if err == nil {
			continue // hello, ping
		}
		if calls.Load() < 5 || websocket.CloseStatus(err) != 4401 {
			t.Fatalf("closed after %d checks with %v", calls.Load(), err)
		}
		return
	}
}
