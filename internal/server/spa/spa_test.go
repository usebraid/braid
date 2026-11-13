// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package spa_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/usebraid/braid/internal/server/spa"
)

func get(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil)) //nolint:noctx // handler test without a network
	return rec
}

func TestServesShellAssetsAndFallback(t *testing.T) {
	h := spa.Handler(fstest.MapFS{
		"index.html":    {Data: []byte(`<!doctype html><script type="module" src="/assets/app.js"></script>`)},
		"assets/app.js": {Data: []byte("console.log(1)")},
	})
	for _, p := range []string{"/", "/timeline", "/devices", "/sessions/abc%2Fdef", "/index.html"} {
		rec := get(h, "GET", p)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "app.js") || rec.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: %d %q %q", p, rec.Code, rec.Header().Get("Cache-Control"), rec.Body.String())
		}
		if rec.Header().Get("Content-Security-Policy") != spa.CSP {
			t.Errorf("%s: CSP %q", p, rec.Header().Get("Content-Security-Policy"))
		}
	}
	js := get(h, "GET", "/assets/app.js")
	if js.Code != 200 || !strings.Contains(js.Header().Get("Cache-Control"), "immutable") || !strings.Contains(js.Header().Get("Content-Type"), "javascript") {
		t.Errorf("asset: %d %v", js.Code, js.Header())
	}
	if rec := get(h, "HEAD", "/timeline"); rec.Code != 200 || rec.Body.Len() != 0 {
		t.Errorf("HEAD: %d", rec.Code)
	}
}

func TestMissingFilesAndAPIPathsAreNotHTML(t *testing.T) {
	h := spa.Handler(fstest.MapFS{"index.html": {Data: []byte("<html>")}})
	for _, p := range []string{"/assets/missing.js", "/missing.css", "/api/v1/nope", "/v1/nope", "/auth/nope", "/device", "/device/nope"} {
		rec := get(h, "GET", p)
		if rec.Code != 404 || strings.Contains(rec.Body.String(), "<html>") {
			t.Errorf("%s: %d %q", p, rec.Code, rec.Body.String())
		}
	}
	if rec := get(h, "POST", "/"); rec.Code != 405 {
		t.Errorf("POST: %d", rec.Code)
	}
}

func TestPlaceholderBuildIs503(t *testing.T) {
	rec := get(spa.Handler(fstest.MapFS{".gitkeep": {}}), "GET", "/")
	b, _ := io.ReadAll(rec.Body)
	if rec.Code != 503 || !strings.Contains(string(b), "not built") {
		t.Fatalf("%d %s", rec.Code, b)
	}
}
