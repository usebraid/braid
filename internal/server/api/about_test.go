// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

//nolint:errcheck,gosec,noctx // test: short-lived requests
package api_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/usebraid/braid/internal/server/api"
	"github.com/usebraid/braid/internal/server/servertest"
)

func TestSourceLink(t *testing.T) {
	for _, c := range []struct{ v, commit, want string }{
		{"dev", "", "https://g.test/x/braid"},
		{"0.3.0", "", "https://g.test/x/braid/releases/tag/v0.3.0"},
		{"v0.3.0", "", "https://g.test/x/braid/releases/tag/v0.3.0"},
		{"0.3.0", "abc123", "https://g.test/x/braid/tree/abc123"},
	} {
		if got := api.SourceLink("https://g.test/x/braid", c.v, c.commit); got != c.want {
			t.Errorf("%s %s: %s", c.v, c.commit, got)
		}
	}
}

func TestAboutRequiresLoginAndReturnsSourceURL(t *testing.T) {
	e := servertest.Start(t)
	resp, err := e.Client().Get(e.URL + "/api/v1/about")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("anonymous: %d", resp.StatusCode)
	}
	r2, err := e.Login(t, "u1").Get(e.URL + "/api/v1/about")
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Body.Close()
	var a api.About
	if r2.StatusCode != 200 || json.NewDecoder(r2.Body).Decode(&a) != nil || !strings.HasPrefix(a.SourceURL, "https://github.com/usebraid/braid") || a.Version == "" {
		t.Fatalf("%d %+v", r2.StatusCode, a)
	}
}
