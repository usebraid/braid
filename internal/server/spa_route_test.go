// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

//nolint:errcheck,noctx,bodyclose,gosec // test code: short-lived loopback requests
package server_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/usebraid/braid/internal/server/servertest"
)

func TestShellRequiresLoginAndHealthzStaysOpen(t *testing.T) {
	e := servertest.Start(t)
	c := e.Client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	for _, p := range []string{"/", "/timeline", "/assets/x.js"} {
		resp, err := c.Get(e.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 302 || !strings.HasPrefix(resp.Header.Get("Location"), "/auth/login?next=") {
			t.Errorf("%s: %d %s", p, resp.StatusCode, resp.Header.Get("Location"))
		}
	}
	if r, err := c.Get(e.URL + "/healthz"); err != nil || r.StatusCode != 200 {
		t.Fatalf("healthz: %v %v", r, err)
	}
	r, err := e.Login(t, "u1").Get(e.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 503 && r.StatusCode != 200 { // 503 until the web app is built
		t.Fatalf("logged in: %d", r.StatusCode)
	}
}
