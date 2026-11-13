// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var png = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{1}, 64)...)

// idp serves the avatar variants on one TLS origin and counts the requests that reach it.
func idp(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/ok.png", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	})
	mux.HandleFunc("/moved.png", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/ok.png", http.StatusFound) })
	mux.HandleFunc("/big.png", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(bytes.Repeat([]byte{1}, avatarMax+1))
	})
	mux.HandleFunc("/big-chunked.png", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		for range 9 { // no Content-Length: the size is only known while reading
			_, _ = w.Write(bytes.Repeat([]byte{1}, 64<<10))
			w.(http.Flusher).Flush()
		}
	})
	mux.HandleFunc("/page.html", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<svg onload=alert(1)>"))
	})
	mux.HandleFunc("/vector.svg", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write([]byte("<svg/>"))
	})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestIssuerAvatarFetchServesTheIssuerOriginOnly(t *testing.T) {
	srv, hits := idp(t)
	other, otherHits := idp(t)
	fetch := IssuerAvatarFetch(srv.URL+"/realms/x", srv.Client().Transport)
	ctx := context.Background()

	body, ct, err := fetch(ctx, srv.URL+"/ok.png?token=secret")
	if err != nil || ct != "image/png" || !bytes.Equal(body, png) {
		t.Fatalf("issuer-origin picture: %q %v", ct, err)
	}
	for name, raw := range map[string]string{
		"foreign origin": other.URL + "/ok.png",
		"plain http":     strings.Replace(srv.URL, "https://", "http://", 1) + "/ok.png",
		"userinfo":       strings.Replace(srv.URL, "https://", "https://u:p@", 1) + "/ok.png",
		"not a URL":      "::",
		"relative":       "/ok.png",
	} {
		before := hits.Load()
		if _, _, err := fetch(ctx, raw); err == nil {
			t.Errorf("%s: fetched", name)
		}
		if hits.Load() != before || otherHits.Load() != 0 {
			t.Errorf("%s: a request left the server", name)
		}
	}
	for name, path := range map[string]string{
		"redirect": "/moved.png", "oversized": "/big.png", "oversized without length": "/big-chunked.png",
		"html": "/page.html", "svg": "/vector.svg", "missing": "/nope.png",
	} {
		_, _, err := fetch(ctx, srv.URL+path)
		if err == nil {
			t.Errorf("%s: accepted", name)
		} else if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), srv.URL) {
			t.Errorf("%s: error names the URL: %v", name, err)
		}
	}
	if _, _, err := IssuerAvatarFetch("http://idp.test", srv.Client().Transport)(ctx, srv.URL+"/ok.png"); err == nil {
		t.Error("an http issuer must not allow any picture")
	}
}

func TestAvatarCachePerUserAndURL(t *testing.T) {
	calls := 0
	now := time.Unix(0, 0)
	c := &avatarCache{m: map[string]avatarEntry{}, now: func() time.Time { return now }, fetch: func(_ context.Context, raw string) ([]byte, string, error) {
		calls++
		if raw == "bad" {
			return nil, "", errAvatar
		}
		return []byte(raw), "image/png", nil
	}}
	get := func(user, raw string) string {
		b, _, ok := c.get(context.Background(), user, raw)
		if !ok {
			return "-"
		}
		return string(b)
	}
	first, second := get("a", "pa"), get("a", "pa")
	if first != "pa" || second != "pa" || calls != 1 {
		t.Fatalf("second read must come from the cache, calls=%d", calls)
	}
	first, second = get("b", "bad"), get("b", "bad")
	if first != "-" || second != "-" || calls != 2 {
		t.Fatalf("a failure is cached briefly too, calls=%d", calls)
	}
	if get("a", "pa2") != "pa2" || calls != 3 {
		t.Fatal("a new picture URL is fetched at once")
	}
	now = now.Add(avatarTTL + time.Second)
	if get("a", "pa2") != "pa2" || calls != 4 {
		t.Fatal("an entry expires after an hour")
	}
}
