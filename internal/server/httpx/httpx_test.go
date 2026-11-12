// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestClientIP(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	r := httptest.NewRequestWithContext(context.Background(), "GET", "/", nil)
	r.RemoteAddr = "203.0.113.5:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := ClientIP(r, trusted); got.String() != "203.0.113.5" {
		t.Errorf("untrusted remote: %s", got)
	}
	r.RemoteAddr = "10.1.1.1:1234"
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 198.51.100.7, 10.2.2.2")
	if got := ClientIP(r, trusted); got.String() != "198.51.100.7" {
		t.Errorf("trusted chain: %s", got)
	}
	r.Header.Del("X-Forwarded-For")
	if got := ClientIP(r, trusted); got.String() != "10.1.1.1" {
		t.Errorf("no header: %s", got)
	}
}

func TestClientIPEdges(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	for _, c := range []struct{ remote, xff, want string }{
		{"10.1.1.1:1", "10.3.3.3, 10.2.2.2", "10.3.3.3"},         // all trusted: leftmost
		{"10.1.1.1:1", "6.6.6.6, garbage, 10.2.2.2", "10.1.1.1"}, // malformed ends walk
		{"10.1.1.1:1", "", "10.1.1.1"},
		{"[::ffff:10.1.1.1]:1", "198.51.100.7", "198.51.100.7"}, // mapped peer is trusted
		{"10.1.1.1:1", "::ffff:10.2.2.2, 1.1.1.1, ::ffff:10.4.4.4", "1.1.1.1"},
	} {
		r := httptest.NewRequestWithContext(context.Background(), "GET", "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := ClientIP(r, trusted); got.String() != c.want {
			t.Errorf("%s %q: %s want %s", c.remote, c.xff, got, c.want)
		}
	}
}

func TestLimiter(t *testing.T) {
	now := time.Unix(0, 0)
	l := NewLimiter(5, func() time.Time { return now })
	for i := 0; i < 5; i++ {
		if !l.Allow("ip") {
			t.Fatalf("request %d denied", i)
		}
	}
	if l.Allow("ip") {
		t.Fatal("6th request allowed")
	}
	if !l.Allow("other") {
		t.Fatal("keys not independent")
	}
	now = now.Add(12 * time.Second) // one token per 12 s at 5/min
	if !l.Allow("ip") {
		t.Fatal("token not refilled")
	}
	if l.Allow("ip") {
		t.Fatal("refilled more than one token")
	}
}

func TestLimiterEvictsIdle(t *testing.T) {
	now := time.Unix(0, 0)
	l := NewLimiter(5, func() time.Time { return now })
	l.Allow("old")
	now = now.Add(11 * time.Minute)
	l.Allow("new")
	if _, ok := l.buckets["old"]; ok {
		t.Error("idle bucket not evicted")
	}
}

func TestTooMany(t *testing.T) {
	w := httptest.NewRecorder()
	TooMany(w, 1500*time.Millisecond)
	if w.Code != 429 || w.Header().Get("Retry-After") != "2" || !strings.Contains(w.Body.String(), `"rate_limited"`) {
		t.Errorf("%d %v %s", w.Code, w.Header(), w.Body)
	}
}

func TestSameOrigin(t *testing.T) {
	pub, _ := url.Parse("https://braid.example.org")
	h := RequireSameOrigin(pub, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	cases := []struct {
		method, origin string
		want           int
	}{
		{"GET", "", 204},
		{"POST", "https://braid.example.org", 204},
		{"POST", "https://evil.example", 403},
		{"POST", "", 403},
		{"DELETE", "https://braid.example.org.evil.example", 403},
		{"POST", "http://braid.example.org", 403},
		{"POST", "https://braid.example.org:8443", 403},
		{"POST", "https://BRAID.example.org/", 403},
	}
	for _, c := range cases {
		r := httptest.NewRequestWithContext(context.Background(), c.method, "/x", nil)
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("%s %q: %d want %d", c.method, c.origin, w.Code, c.want)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	SecurityHeaders(http.NotFoundHandler()).ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), "GET", "/", nil))
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Error("CSP missing")
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("nosniff missing")
	}
	if w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Error("referrer policy missing")
	}
}

func TestReadJSON(t *testing.T) {
	var v struct {
		A int `json:"a"`
	}
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/", strings.NewReader(`{"a":1}`))
	if err := ReadJSON(r, 100, &v); err != nil || v.A != 1 {
		t.Fatalf("%v %v", v, err)
	}
	r = httptest.NewRequestWithContext(context.Background(), "POST", "/", strings.NewReader(`{"a":1,"b":2}`))
	if err := ReadJSON(r, 100, &v); err == nil {
		t.Error("unknown field accepted")
	}
	r = httptest.NewRequestWithContext(context.Background(), "POST", "/", strings.NewReader(`{"a":1}{"a":2}`))
	if err := ReadJSON(r, 100, &v); err == nil {
		t.Error("trailing value accepted")
	}
	r = httptest.NewRequestWithContext(context.Background(), "POST", "/", strings.NewReader(`{"a":`+strings.Repeat("1", 200)+`}`))
	if err := ReadJSON(r, 100, &v); err == nil {
		t.Error("oversized body accepted")
	}
}

func TestLimiterAllowedDoesNotConsume(t *testing.T) {
	l := NewLimiter(1, time.Now)
	for i := 0; i < 3; i++ {
		if !l.Allowed("k") {
			t.Fatal("Allowed consumed or denied a fresh key")
		}
	}
	l.Allow("k")
	if l.Allowed("k") {
		t.Fatal("Allowed true on empty bucket")
	}
}
