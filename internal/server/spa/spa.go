// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package spa serves the embedded single-page web app.
package spa

import (
	"bytes"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/usebraid/braid/internal/server/httpx"
)

// CSP is the policy for the web app: same-origin only, no inline script or style, no framing.
const CSP = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; manifest-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

// Handler serves files from files, falling back to index.html for client-side routes.
// Paths of the API, device and auth endpoints that reach it answer 404.
func Handler(files fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Security-Policy", CSP)
		p := path.Clean("/" + r.URL.Path)
		switch {
		case strings.HasPrefix(p, "/api/"):
			httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		case strings.HasPrefix(p, "/v1/"), strings.HasPrefix(p, "/auth/"), strings.HasPrefix(p, "/device"):
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(p, "/")
		if name != "" && name != "index.html" {
			if b, err := fs.ReadFile(files, name); err == nil {
				cache := "no-cache"
				if strings.HasPrefix(name, "assets/") {
					cache = "private, max-age=31536000, immutable"
				}
				w.Header().Set("Cache-Control", cache)
				http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(b))
				return
			}
			if path.Ext(name) != "" { // a missing script or stylesheet must not come back as HTML
				http.NotFound(w, r)
				return
			}
		}
		index, err := fs.ReadFile(files, "index.html")
		if err != nil {
			http.Error(w, "web app not built", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
	})
}
