// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package stats_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/usebraid/braid/internal/server/stats"
)

func TestSearch(t *testing.T) {
	x := newEnv(t)
	seed(t, x)
	search := func(q string, extra ...string) stats.SearchResult {
		t.Helper()
		var r stats.SearchResult
		path := "/api/v1/stats/search?q=" + url.QueryEscape(q)
		for _, e := range extra {
			path += "&" + e
		}
		if st := x.get(t, path, &r); st != 200 {
			t.Fatalf("%q: status %d", q, st)
		}
		return r
	}
	if r := search("zebra"); len(r.Hits) != 1 || r.Hits[0].SessionID != "s1" || r.Hits[0].Role != "user" || r.Hits[0].AgentID != "main" ||
		r.Hits[0].Title != "Alpha work" || !strings.Contains(r.Hits[0].Snippet, "zebra") || strings.ContainsAny(r.Hits[0].Snippet, "{}") {
		t.Fatalf("%+v", r.Hits)
	}
	if r := search("quokka"); len(r.Hits) != 1 || r.Hits[0].Role != "assistant" || !strings.Contains(r.Hits[0].Snippet, "quokka") {
		t.Fatalf("%+v", r.Hits)
	}
	if r := search("handl"); len(r.Hits) != 1 || r.Hits[0].AgentID != "a1" || !strings.Contains(r.Hits[0].Snippet, "handler.go") { // partial identifier via pg_trgm
		t.Fatalf("%+v", r.Hits)
	}
	if r := search("bash"); len(r.Hits) != 1 || r.Hits[0].SessionID != "s1" { // tool names are searchable
		t.Fatalf("%+v", r.Hits)
	}
	if r := search("beta"); len(r.Hits) != 3 {
		t.Fatalf("%+v", r.Hits)
	}
	if r := search("beta", "limit=2"); len(r.Hits) != 2 {
		t.Fatalf("%+v", r.Hits)
	}
	if r := search("zebra", "project="+url.QueryEscape("/work/beta")); len(r.Hits) != 0 {
		t.Fatalf("filter ignored: %+v", r.Hits)
	}
	if r := search("nothingmatches"); r.Hits == nil || len(r.Hits) != 0 {
		t.Fatalf("%+v", r.Hits)
	}
	if r := search("100%"); len(r.Hits) != 0 { // wildcards are literals
		t.Fatalf("%+v", r.Hits)
	}
	for _, q := range []string{"", "a", strings.Repeat("x", 201)} {
		if st := x.get(t, "/api/v1/stats/search?q="+url.QueryEscape(q), nil); st != 400 {
			t.Errorf("%q: status %d", q, st)
		}
	}
}
