// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package stats_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/usebraid/braid/internal/server/stats"
)

func TestFailureAndSearchCarryLineIDsAndLinesPageBackwards(t *testing.T) {
	x := newEnv(t)
	seed(t, x)
	var fp stats.FailurePage
	x.get(t, "/api/v1/stats/failures?"+rng+"&limit=200", &fp)
	var fid int64
	for _, f := range fp.Items {
		if f.SessionID == "s1" && f.Category == "server_error" {
			fid = f.LineID
		}
	}
	if fid == 0 {
		t.Fatalf("no line id: %+v", fp.Items)
	}
	var page stats.LinePage
	x.get(t, "/api/v1/stats/sessions/s1/lines?agent=main&after="+strconv.FormatInt(fid-1, 10)+"&limit=1", &page)
	if len(page.Lines) != 1 || page.Lines[0].ID != fid || page.Prev == nil {
		t.Fatalf("jump: %+v", page)
	}
	if !strings.Contains(string(page.Lines[0].Line), "server_error") {
		t.Fatalf("not the failure line: %s", page.Lines[0].Line)
	}
	var back stats.LinePage
	x.get(t, "/api/v1/stats/sessions/s1/lines?agent=main&before="+strconv.FormatInt(fid, 10)+"&limit=2", &back)
	if len(back.Lines) != 2 || back.Lines[0].ID >= back.Lines[1].ID || back.Lines[1].ID >= fid || back.Prev == nil {
		t.Fatalf("before: %+v", back)
	}
	var first stats.LinePage
	x.get(t, "/api/v1/stats/sessions/s1/lines?agent=main&limit=1", &first)
	if first.Prev != nil {
		t.Fatalf("first page has prev: %+v", first)
	}
	if st := x.get(t, "/api/v1/stats/sessions/s1/lines?agent=main&after=1&before=9", nil); st != 400 {
		t.Fatalf("after+before: %d", st)
	}
	var sr stats.SearchResult
	x.get(t, "/api/v1/stats/search?q=zebra&"+rng, &sr)
	if len(sr.Hits) == 0 || sr.Hits[0].LineID == 0 {
		t.Fatalf("search hit without line id: %+v", sr)
	}
	var hit stats.LinePage
	x.get(t, "/api/v1/stats/sessions/s1/lines?agent=main&after="+strconv.FormatInt(sr.Hits[0].LineID-1, 10)+"&limit=1", &hit)
	if len(hit.Lines) != 1 || !strings.Contains(string(hit.Lines[0].Line), "zebra") {
		t.Fatalf("hit line: %+v", hit)
	}
}
