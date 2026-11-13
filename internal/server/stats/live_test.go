// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package stats_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	dt "github.com/usebraid/braid/internal/server/derive/derivetest"
	"github.com/usebraid/braid/internal/server/stats"
)

func TestLive(t *testing.T) {
	x := newEnv(t)
	now := time.Now().UTC()
	ts := func(d time.Duration) string { return now.Add(d).Format("2006-01-02T15:04:05.000Z") }
	x.dt.Put(t, x.uid, x.dev,
		dt.File{Rel: "-work-live/l1.jsonl", Lines: []string{
			dt.Prompt(ts(-30*time.Second), "live prompt"),
			dt.Reply(ts(-10*time.Second), "lm1", opus, dt.Usage(10, 5, 0, 0, 0), dt.Tool("run1", "Bash", map[string]any{"command": "sleep 60"})),
		}},
		dt.File{Rel: "-work-old/o1.jsonl", Lines: []string{
			dt.Prompt(ts(-10*time.Minute), "old prompt"),
			dt.Reply(ts(-9*time.Minute), "om1", opus, dt.Usage(10, 5, 0, 0, 0), dt.Text("done")),
		}})
	x.dt.Derive(t, x.uid, 0)
	var lp stats.LivePage
	if st := x.get(t, "/api/v1/stats/live", &lp); st != 200 || len(lp.Items) != 1 {
		t.Fatalf("%d %+v", st, lp)
	}
	it := lp.Items[0]
	if it.Session.SessionID != "l1" || !it.Session.IsLive || len(it.Agents) != 1 || it.Agents[0].AgentID != "main" {
		t.Fatalf("%+v", it)
	}
	if ct := it.Agents[0].CurrentTool; ct == nil || ct.Name != "Bash" || !strings.Contains(ct.Summary, "sleep") {
		t.Fatalf("current tool: %+v", ct)
	}
	x.dt.Put(t, x.uid, x.dev, dt.File{Rel: "-work-live/l1.jsonl", Lines: []string{dt.Result(ts(-5*time.Second), "run1", false, map[string]any{})}})
	x.dt.Derive(t, x.uid, 0)
	x.get(t, "/api/v1/stats/live", &lp)
	if len(lp.Items) != 1 || lp.Items[0].Agents[0].CurrentTool != nil {
		t.Fatalf("tool should be finished: %+v", lp)
	}
}

func TestLiveIsPerUser(t *testing.T) {
	x := newEnv(t)
	x.dt.Put(t, x.uid, x.dev, dt.File{Rel: "-work-live/l1.jsonl", Lines: []string{dt.Prompt(time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), "mine")}})
	x.dt.Derive(t, x.uid, 0)
	var lp stats.LivePage
	if st := x.getAs(t, x.Login(t, "u2"), "/api/v1/stats/live", &lp); st != 200 || len(lp.Items) != 0 {
		t.Fatalf("leak: %d %+v", st, lp)
	}
}

func TestFilterOptions(t *testing.T) {
	x := newEnv(t)
	seed(t, x)
	var fo stats.FilterOptions
	if st := x.get(t, "/api/v1/stats/filters?"+rng, &fo); st != 200 {
		t.Fatal(st)
	}
	if !slices.Equal(fo.Models, []string{opus, sonnet}) || !slices.Equal(fo.Skills, []string{"superpowers:writing-plans"}) ||
		len(fo.Projects) != 2 || fo.Projects[0] != (stats.ProjectOption{Path: "/work/alpha", Name: "alpha"}) || fo.Projects[1].Path != "/work/beta" {
		t.Fatalf("%+v", fo)
	}
	if st := x.get(t, "/api/v1/stats/filters?from=2026-12-01T00:00:00Z&to=2026-11-01T00:00:00Z", nil); st != 400 {
		t.Fatalf("bad range: %d", st)
	}
}

func TestLiveShowsTheCurrentToolOfEveryActiveAgent(t *testing.T) {
	x := newEnv(t)
	now := time.Now().UTC()
	ts := func(d time.Duration) string { return now.Add(d).Format("2006-01-02T15:04:05.000Z") }
	files := []dt.File{{Rel: "-work-live/l1.jsonl", Lines: []string{
		dt.Prompt(ts(-30*time.Second), "p"),
		dt.Reply(ts(-10*time.Second), "m-main", opus, dt.Usage(1, 1, 0, 0, 0), dt.Tool("t-main", "Bash", map[string]any{"command": "main"})),
	}}}
	for _, a := range []string{"a1", "a2", "a3"} {
		files = append(files,
			dt.File{Rel: "-work-live/l1/subagents/agent-" + a + ".meta.json", JSON: `{"agentType":"Explore","description":"d"}`},
			dt.File{Rel: "-work-live/l1/subagents/agent-" + a + ".jsonl", Lines: []string{
				dt.Side(dt.SidePrompt(ts(-20*time.Second), "task"), a),
				dt.Side(dt.Reply(ts(-9*time.Second), "m-"+a, opus, dt.Usage(1, 1, 0, 0, 0), dt.Tool("t-"+a, "Read", map[string]any{"file_path": a})), a)}})
	}
	x.dt.Put(t, x.uid, x.dev, files...)
	x.dt.Derive(t, x.uid, 0)
	var lp stats.LivePage
	if st := x.get(t, "/api/v1/stats/live", &lp); st != 200 || len(lp.Items) != 1 || len(lp.Items[0].Agents) != 4 {
		t.Fatalf("%d %+v", st, lp)
	}
	for _, a := range lp.Items[0].Agents {
		if a.CurrentTool == nil {
			t.Errorf("agent %s has no current tool", a.AgentID)
		}
	}
}
