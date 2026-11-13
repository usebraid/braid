// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package httpx

import (
	"net/http"
	"net/url"
	"strings"
)

// UserAgent returns the request's User-Agent for persistence: at most 256 bytes,
// with invalid UTF-8 (including a rune cut by the limit) removed (Postgres rejects it).
func UserAgent(r *http.Request) string {
	ua := r.UserAgent()
	if len(ua) > 256 {
		ua = ua[:256]
	}
	return strings.ToValidUTF8(ua, "")
}

// SecurityHeaders sets the fixed response security headers.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// RequireSameOrigin rejects state-changing requests whose Origin is not exactly
// the scheme and host (with port) of public; a missing Origin is rejected.
func RequireSameOrigin(public *url.URL, next http.Handler) http.Handler {
	want := public.Scheme + "://" + public.Host
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if r.Header.Get("Origin") != want {
				WriteJSON(w, http.StatusForbidden, map[string]string{"error": "bad_origin"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
