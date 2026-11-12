// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package stats_test

import (
	"net/url"
	"reflect"
	"testing"

	dt "github.com/usebraid/braid/internal/server/derive/derivetest"
	"github.com/usebraid/braid/internal/server/stats"
)

func TestFailuresEndpoint(t *testing.T) {
	x := newEnv(t)
	seed(t, x)
	var fp stats.FailurePage
	if st := x.get(t, "/api/v1/stats/failures?"+rng, &fp); st != 200 || len(fp.Items) != 3 {
		t.Fatalf("%d %+v", st, fp)
	}
	u := fp.Items[0] // newest first: the usage limit of s2
	if u.Category != "usage_limit" || u.LimitType == nil || *u.LimitType != "five_hour" || u.TimeLostS == nil || *u.TimeLostS != 1800 || u.SessionID != "s2" || u.ResetsAt == nil {
		t.Fatalf("%+v", u) // resets after 7200 s but the next successful message came after 1800 s
	}
	cat := map[string]stats.FailureAgg{}
	for _, a := range fp.ByCategory {
		cat[a.Key] = a
	}
	if len(cat) != 3 || cat["usage_limit"].Count != 1 || cat["usage_limit"].TimeLostS != 1800 || cat["tool_error"].Count != 1 || cat["server_error"].TimeLostS != 0 {
		t.Fatalf("%+v", fp.ByCategory)
	}
	if !reflect.DeepEqual(fp.ByLimitType, []stats.FailureAgg{{Key: "five_hour", Count: 1, TimeLostS: 1800}}) {
		t.Fatalf("%+v", fp.ByLimitType)
	}
	if !reflect.DeepEqual(fp.ByDay, []stats.FailureAgg{{Key: "2026-11-01", Count: 2}, {Key: "2026-11-02", Count: 1, TimeLostS: 1800}}) || fp.TimeLostS != 1800 {
		t.Fatalf("%+v %d", fp.ByDay, fp.TimeLostS)
	}
	var p1, p2 stats.FailurePage
	x.get(t, "/api/v1/stats/failures?"+rng+"&limit=2", &p1)
	if len(p1.Items) != 2 || p1.Next == nil || len(p1.ByCategory) != 3 {
		t.Fatalf("%+v", p1)
	}
	x.get(t, "/api/v1/stats/failures?"+rng+"&limit=2&cursor="+url.QueryEscape(*p1.Next), &p2)
	if len(p2.Items) != 1 || p2.Next != nil {
		t.Fatalf("%+v", p2)
	}
	var beta stats.FailurePage
	x.get(t, "/api/v1/stats/failures?"+rng+"&project="+url.QueryEscape("/work/beta"), &beta)
	if len(beta.Items) != 1 || len(beta.ByCategory) != 1 {
		t.Fatalf("%+v", beta)
	}
}

func TestTimeLostIsClippedByAnySuccessfulMessageOfTheUser(t *testing.T) {
	x := newEnv(t)
	limit := func(ts string, resets int64) string {
		return dt.APIError(ts, "error", "rate_limit", "apiError", "usage_limit_reached",
			"quotaLimits", map[string]any{"resetsAt": resets, "rateLimitType": "seven_day"})
	}
	x.dt.Put(t, x.uid, x.dev,
		dt.File{Rel: "-work-alpha/a.jsonl", Lines: []string{dt.Prompt(dt.At(0), "p"), limit(dt.At(10), dt.Unix(7210))}},            // would be 7200 s
		dt.File{Rel: "-work-alpha/b.jsonl", Lines: []string{dt.Reply(dt.At(1810), "mb", opus, dt.Usage(1, 1, 0, 0, 0))}},           // another session works again after 1800 s
		dt.File{Rel: "-work-alpha/c.jsonl", Lines: []string{dt.Prompt(dt.At(100000), "p"), limit(dt.At(100010), dt.Unix(107210))}}) // nothing follows: full 7200 s
	x.dt.Derive(t, x.uid, 0)
	var fp stats.FailurePage
	x.get(t, "/api/v1/stats/failures?"+rng, &fp)
	if len(fp.Items) != 2 || *fp.Items[0].TimeLostS != 7200 || *fp.Items[1].TimeLostS != 1800 || fp.TimeLostS != 9000 {
		t.Fatalf("%+v", fp)
	}
}
