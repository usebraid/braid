// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package stats_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	dt "github.com/usebraid/braid/internal/server/derive/derivetest"
	"github.com/usebraid/braid/internal/server/stats"
	"github.com/usebraid/braid/internal/testdb"
)

func TestStreak(t *testing.T) {
	d := func(s string) time.Time { v, _ := time.Parse("2006-01-02", s); return v }
	today := d("2026-11-12")
	cases := []struct {
		name string
		days []string
		want int
	}{
		{"none", nil, 0},
		{"today only", []string{"2026-11-12"}, 1},
		{"three ending today", []string{"2026-11-10", "2026-11-11", "2026-11-12"}, 3},
		{"ending yesterday keeps the streak", []string{"2026-11-10", "2026-11-11"}, 2},
		{"a gap breaks it", []string{"2026-11-08", "2026-11-09", "2026-11-11", "2026-11-12"}, 2},
		{"stale", []string{"2026-11-01"}, 0},
		{"unordered duplicates", []string{"2026-11-12", "2026-11-11", "2026-11-11"}, 2},
	}
	for _, c := range cases {
		var days []time.Time
		for _, s := range c.days {
			days = append(days, d(s))
		}
		if got := stats.Streak(days, today); got != c.want {
			t.Errorf("%s: %d want %d", c.name, got, c.want)
		}
	}
}

func TestMergeSeconds(t *testing.T) {
	at := func(s int) time.Time { return time.Unix(int64(s), 0) }
	iv := func(a, b int) stats.Interval { return stats.Interval{Start: at(a), End: at(b)} }
	for name, c := range map[string]struct {
		in   []stats.Interval
		want int64
	}{
		"empty": {nil, 0}, "single": {[]stats.Interval{iv(0, 10)}, 10}, "overlap": {[]stats.Interval{iv(0, 10), iv(5, 20)}, 20},
		"disjoint": {[]stats.Interval{iv(0, 10), iv(20, 30)}, 20}, "contained": {[]stats.Interval{iv(0, 100), iv(10, 20)}, 100},
		"reversed ignored": {[]stats.Interval{iv(10, 0), iv(0, 5)}, 5}, "unordered": {[]stats.Interval{iv(20, 30), iv(0, 10), iv(8, 22)}, 30},
	} {
		if got := stats.MergeSeconds(c.in); got != c.want {
			t.Errorf("%s: %d want %d", name, got, c.want)
		}
	}
}

func TestOverview(t *testing.T) {
	x := newEnv(t)
	x.setTZ(t, "Europe/Berlin") // CET in November: UTC+1
	limitReset := time.Date(2026, 11, 2, 3, 40, 0, 0, time.UTC).Unix()
	x.dt.Put(t, x.uid, x.dev,
		dt.File{Rel: "-work-alpha/s1.jsonl", Lines: []string{
			dt.Prompt("2026-11-01T23:20:00.000Z", "one"),
			dt.Reply("2026-11-01T23:30:00.000Z", "m1", opus, dt.Usage(1_000_000, 100_000, 0, 0, 0), dt.Text("a")), // USD 6
			dt.APIError("2026-11-01T23:40:00.000Z", "error", "rate_limit", "apiError", "usage_limit_reached",
				"quotaLimits", map[string]any{"resetsAt": limitReset, "rateLimitType": "five_hour"}),
			dt.Turn("2026-11-01T23:41:00.000Z", 60000),
		}},
		dt.File{Rel: "-work-alpha/s2.jsonl", Lines: []string{
			dt.Prompt("2026-11-03T09:50:00.000Z", "two"),
			dt.Reply("2026-11-03T10:00:00.000Z", "m2", opus, dt.Usage(1_000_000, 100_000, 0, 0, 0), dt.Text("b")), // USD 6
			dt.Turn("2026-11-03T10:01:00.000Z", 120000),
		}})
	x.dt.Derive(t, x.uid, 0)

	var ov stats.Overview
	if st := x.get(t, "/api/v1/stats/overview?"+rng, &ov); st != 200 {
		t.Fatalf("status %d", st)
	}
	k := ov.KPI
	if ov.TimeZone != "Europe/Berlin" || ov.Reindexing || k.Sessions != 2 || k.AgentMs != 180000 || k.TotalAgentMs != 180000 || k.AgentMsEstimated ||
		!near(k.CostUSD, 12) || k.Tokens != 2_200_000 || k.Failures != 1 || k.TimeLostS != 14400 || k.StreakDays != 0 || len(k.CostUnknownModels) != 0 {
		t.Fatalf("%+v", ov)
	}
	if len(ov.PerDay) != 2 || ov.PerDay[0].Day != "2026-11-02" || ov.PerDay[1].Day != "2026-11-03" || // 23:30Z is already Nov 2 in Berlin
		!near(ov.PerDay[0].CostUSD, 6) || ov.PerDay[0].Tokens != 1_100_000 || ov.PerDay[0].Sessions != 1 {
		t.Fatalf("per_day %+v", ov.PerDay)
	}
	if len(ov.PerModel) != 1 || ov.PerModel[0].Model != opus || ov.PerModel[0].Messages != 2 || ov.PerModel[0].PriceUnknown {
		t.Fatalf("per_model %+v", ov.PerModel)
	}
	if len(ov.PerProject) != 1 || ov.PerProject[0].Path != "/work/alpha" || ov.PerProject[0].Name != "alpha" || ov.PerProject[0].Sessions != 2 || ov.PerProject[0].AgentMs != 180000 {
		t.Fatalf("per_project %+v", ov.PerProject)
	}
	if want := []stats.HeatCell{{Weekday: 1, Hour: 0, Count: 1}, {Weekday: 2, Hour: 11, Count: 1}}; !reflect.DeepEqual(ov.Heatmap, want) { // Mon 00:30, Tue 11:00 Berlin
		t.Fatalf("heatmap %+v", ov.Heatmap)
	}

	var narrowed, none stats.Overview
	x.get(t, "/api/v1/stats/overview?from=2026-11-03T00:00:00Z&to=2026-12-01T00:00:00Z", &narrowed)
	if narrowed.KPI.Sessions != 1 || !near(narrowed.KPI.CostUSD, 6) || narrowed.KPI.Failures != 0 || narrowed.KPI.TimeLostS != 0 {
		t.Fatalf("narrowed %+v", narrowed.KPI)
	}
	x.get(t, "/api/v1/stats/overview?"+rng+"&model=claude-haiku-4-5", &none)
	if none.KPI.Sessions != 0 || none.KPI.CostUSD != 0 || none.PerDay == nil || len(none.PerDay) != 0 {
		t.Fatalf("model filter %+v", none)
	}

	testdb.Exec(t, x.OwnerURL, `INSERT INTO derive_state(user_id, parser_version, rebuilding) VALUES ($1, 1, true)`, x.uid)
	x.get(t, "/api/v1/stats/overview?"+rng, &ov)
	if !ov.Reindexing {
		t.Fatal("rebuilding flag not reported")
	}
}

func TestOverviewValidationAndAuth(t *testing.T) {
	x := newEnv(t)
	for _, q := range []string{"from=garbage", "from=2026-12-01T00:00:00Z&to=2026-11-01T00:00:00Z", "device=not-a-uuid",
		"from=2000-01-01T00:00:00Z&to=2026-01-01T00:00:00Z", "model=" + strings.Repeat("m", 300)} {
		if st := x.get(t, "/api/v1/stats/overview?"+q, nil); st != 400 {
			t.Errorf("%s: status %d", q, st)
		}
	}
	if st := x.getAs(t, x.Client(), "/api/v1/stats/overview", nil); st != 401 {
		t.Errorf("anonymous: status %d", st)
	}
}

func projectSplitSeed(t *testing.T, x *env, n int) {
	t.Helper()
	var files []dt.File
	for i := 1; i <= n; i++ { // project i has i*100k tokens a day; odd ones a second day (so p09 > p07 > p05 = p10 > p08 > p03 = p06 > p04 > p02 = p01 by cost)
		cwd := func(l string) string { return dt.Cwd(l, fmt.Sprintf("/work/p%02d", i)) }
		lines := []string{
			cwd(dt.Prompt(dt.At(i), "p")),
			cwd(dt.Reply(dt.At(i+10), fmt.Sprintf("a%d", i), opus, dt.Usage(i*100_000, 0, 0, 0, 0), dt.Text("x"))),
		}
		if i%2 == 1 {
			lines = append(lines, cwd(dt.Reply(dt.At(day(1)+i), fmt.Sprintf("b%d", i), opus, dt.Usage(i*100_000, 0, 0, 0, 0), dt.Text("y"))))
		}
		files = append(files, dt.File{Rel: fmt.Sprintf("-work-p%02d/s%d.jsonl", i, i), Lines: lines})
	}
	x.dt.Put(t, x.uid, x.dev, files...)
	x.dt.Derive(t, x.uid, 0)
}

func checkSplit(t *testing.T, ov stats.Overview) {
	t.Helper()
	sp := ov.PerDayByProject
	if len(sp.Days) != len(ov.PerDay) {
		t.Fatalf("days %d vs per_day %d", len(sp.Days), len(ov.PerDay))
	}
	for i, d := range sp.Days {
		var c float64
		var tok int64
		if len(d.Values) != len(sp.Projects) {
			t.Fatalf("day %s: %d values for %d projects", d.Day, len(d.Values), len(sp.Projects))
		}
		for _, v := range d.Values {
			c += v.CostUSD
			tok += v.Tokens
		}
		if d.Day != ov.PerDay[i].Day || !near(c, ov.PerDay[i].CostUSD) || tok != ov.PerDay[i].Tokens {
			t.Errorf("day %s: split %v/%d vs per_day %+v", d.Day, c, tok, ov.PerDay[i])
		}
	}
}

func TestOverviewPerDayByProject(t *testing.T) {
	x := newEnv(t)
	projectSplitSeed(t, x, 10)
	var ov stats.Overview
	if st := x.get(t, "/api/v1/stats/overview?"+rng, &ov); st != 200 {
		t.Fatalf("status %d", st)
	}
	checkSplit(t, ov)
	sp := ov.PerDayByProject
	if len(sp.Projects) != 9 || sp.Projects[0].Key != "/work/p09" || sp.Projects[7].Key != "/work/p04" || sp.Projects[8] != (stats.ProjectKey{Key: "_other", Name: "Other"}) {
		t.Fatalf("projects %+v", sp.Projects)
	}
	// p01 (two days of 100k) and p02 (200k, both lowest by cost) fold into Other
	var other int64
	for _, d := range sp.Days {
		other += d.Values[8].Tokens
	}
	if other != 400_000 {
		t.Fatalf("other tokens %d", other)
	}

	var few, filtered, none stats.Overview
	x2 := newEnv(t)
	projectSplitSeed(t, x2, 3)
	x2.get(t, "/api/v1/stats/overview?"+rng, &few)
	checkSplit(t, few)
	if len(few.PerDayByProject.Projects) != 3 || few.PerDayByProject.Projects[0].Key != "/work/p03" {
		t.Fatalf("<=8 projects: %+v", few.PerDayByProject.Projects)
	}
	x.get(t, "/api/v1/stats/overview?"+rng+"&device="+x.dev+"&model="+opus, &filtered)
	checkSplit(t, filtered)
	if len(filtered.PerDayByProject.Projects) != 9 {
		t.Fatalf("device+model filter: %+v", filtered.PerDayByProject.Projects)
	}
	x.get(t, "/api/v1/stats/overview?"+rng+"&model=claude-haiku-4-5", &none)
	checkSplit(t, none)
	if len(none.PerDayByProject.Projects) != 0 || none.PerDayByProject.Days == nil {
		t.Fatalf("model filter: %+v", none.PerDayByProject)
	}
	x.get(t, "/api/v1/stats/overview?"+rng+"&project=/work/p05", &filtered)
	checkSplit(t, filtered)
	if len(filtered.PerDayByProject.Projects) != 1 || filtered.PerDayByProject.Projects[0].Key != "/work/p05" {
		t.Fatalf("project filter: %+v", filtered.PerDayByProject.Projects)
	}
}

func TestOverviewPerDayByProjectIsolation(t *testing.T) {
	x := newEnv(t)
	projectSplitSeed(t, x, 3)
	var ov stats.Overview
	x.getAs(t, x.Login(t, "u2"), "/api/v1/stats/overview?"+rng, &ov)
	if len(ov.PerDayByProject.Projects) != 0 || len(ov.PerDayByProject.Days) != 0 {
		t.Fatalf("leak: %+v", ov.PerDayByProject)
	}
}
