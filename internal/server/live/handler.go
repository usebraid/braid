// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package live

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/coder/websocket"
	"github.com/usebraid/braid/internal/server/httpx"
	"github.com/usebraid/braid/internal/server/webauth"
)

const (
	protocolVersion = 1
	readLimit       = 256
	maxFrames       = 20 // client frames per frameWindow
	frameWindow     = 10 * time.Second
	writeTimeout    = 10 * time.Second

	closeSessionEnded websocket.StatusCode = 4401
	closeTooManyFrame websocket.StatusCode = 4429
)

// Handler serves the WebSocket. It authenticates itself (session cookie, exact same-origin Origin).
func (h *Hub) Handler(public *url.URL, check func(*http.Request) (webauth.Principal, bool, error)) http.Handler {
	origin := public.Scheme + "://" + public.Host
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok, err := check(r)
		if err != nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "unavailable"})
			return
		}
		if !ok {
			httpx.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
			return
		}
		if o := r.Header.Values("Origin"); len(o) != 1 || o[0] != origin {
			httpx.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "bad_origin"})
			return
		}
		select {
		case <-h.ready:
		default:
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "unavailable"})
			return
		}
		if !h.limiter.Allow(p.UserID) {
			httpx.TooMany(w, time.Minute)
			return
		}
		ob, ok := h.add(p.UserID)
		if !ok {
			httpx.TooMany(w, 5*time.Second)
			return
		}
		defer h.remove(p.UserID, ob)
		// The server-wide read/write deadlines would kill a long-lived socket.
		rc := http.NewResponseController(w)
		_ = rc.SetReadDeadline(time.Time{})
		_ = rc.SetWriteDeadline(time.Time{})
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			CompressionMode:    websocket.CompressionDisabled,
			InsecureSkipVerify: true, //nolint:gosec // Origin verified exactly above
		})
		if err != nil {
			return // Accept already answered
		}
		defer func() { _ = c.CloseNow() }()
		c.SetReadLimit(readLimit)
		h.serve(r, c, p, ob, check)
	})
}

func (h *Hub) serve(r *http.Request, c *websocket.Conn, p webauth.Principal, ob *outbox, check func(*http.Request) (webauth.Principal, bool, error)) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	write := func(m Message) error {
		b, _ := json.Marshal(m)
		wctx, wc := context.WithTimeout(ctx, writeTimeout)
		defer wc()
		return c.Write(wctx, websocket.MessageText, b)
	}
	// Reader: discards frames, answers control frames, enforces the frame rate.
	go func() {
		defer cancel()
		start, n := time.Now(), 0
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
			}
			if time.Since(start) > frameWindow {
				start, n = time.Now(), 0
			}
			if n++; n > maxFrames {
				_ = c.Close(closeTooManyFrame, "too many frames")
				return
			}
		}
	}()
	if write(Message{Type: "hello", Protocol: protocolVersion}) != nil {
		return
	}
	ping, recheck := time.NewTicker(h.o.PingEvery), time.NewTicker(h.o.RecheckEvery)
	defer ping.Stop()
	defer recheck.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.stop:
			_ = c.Close(websocket.StatusGoingAway, "server shutdown")
			return
		case <-ob.wake:
			for m, ok := ob.pop(); ok; m, ok = ob.pop() {
				if write(m) != nil {
					return
				}
			}
		case <-ping.C:
			if write(Message{Type: "ping"}) != nil {
				return
			}
		case <-recheck.C:
			np, ok, err := check(r.Clone(ctx))
			if err != nil { // a database hiccup is not an expired session: keep the socket, look again next time
				continue
			}
			if !ok || np.UserID != p.UserID {
				_ = c.Close(closeSessionEnded, "session ended")
				return
			}
		}
	}
}
