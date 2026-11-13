// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

//nolint:errcheck,bodyclose // test code: deferred best-effort close
package live

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
	srv.Config.Handler = h.Handler(pub, func(*http.Request) (webauth.Principal, bool) { return webauth.Principal{UserID: "u1"}, true })
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
