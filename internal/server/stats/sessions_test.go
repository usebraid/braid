// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package stats_test

import (
	"encoding/json"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/usebraid/braid/internal/protocol"
	dt "github.com/usebraid/braid/internal/server/derive/derivetest"
	"github.com/usebraid/braid/internal/server/stats"
)

func ids(p stats.SessionPage) []string {
	out := []string{}
	for _, s := range p.Items {
		out = append(out, s.SessionID)
	}
	return out
}

func TestSessionList(t *testing.T) {
	x := newEnv(t)
	d1, d2 := seed(t, x)
	var pg stats.SessionPage
	if st := x.get(t, "/api/v1/stats/sessions?"+rng, &pg); st != 200 || !slices.Equal(ids(pg), []string{"s3", "s2", "s1"}) || pg.Next != nil {
		t.Fatalf("%d %v", st, ids(pg))
	}
	s1 := pg.Items[2]
	if s1.Title != "Alpha work" || s1.Project != "/work/alpha" || s1.ProjectName != "alpha" || s1.AgentMs != 3000 || s1.TotalAgentMs != 8000 || s1.AgentMsEstimated ||
		s1.Tokens != 3850 || s1.Failures != 2 || s1.LineCount != 11 || s1.IsLive || s1.GitBranch != "main" || !near(s1.CostUSD, 0.0195) ||
		!slices.Equal(s1.Models, []string{opus, sonnet}) || !slices.Equal(s1.Skills, []string{"superpowers:writing-plans"}) ||
		s1.DeviceID == nil || *s1.DeviceID != d1 || !slices.Equal(s1.DeviceIDs, []string{d1}) {
		t.Fatalf("%+v", s1)
	}
	if pg.Items[0].Title != "Custom gamma" || pg.Items[0].DeviceID == nil || *pg.Items[0].DeviceID != d2 {
		t.Fatalf("%+v", pg.Items[0])
	}

	var p1, p2 stats.SessionPage // pagination
	x.get(t, "/api/v1/stats/sessions?"+rng+"&limit=2", &p1)
	if !slices.Equal(ids(p1), []string{"s3", "s2"}) || p1.Next == nil {
		t.Fatalf("%v %v", ids(p1), p1.Next)
	}
	x.get(t, "/api/v1/stats/sessions?"+rng+"&limit=2&cursor="+url.QueryEscape(*p1.Next), &p2)
	if !slices.Equal(ids(p2), []string{"s1"}) || p2.Next != nil {
		t.Fatalf("%v %v", ids(p2), p2.Next)
	}

	for name, c := range map[string]struct {
		q    string
		want []string
	}{
		"project":       {rng + "&project=" + url.QueryEscape("/work/beta"), []string{"s2"}},
		"model":         {rng + "&model=" + sonnet, []string{"s1"}},
		"skill":         {rng + "&skill=" + url.QueryEscape("superpowers:writing-plans"), []string{"s1"}},
		"device":        {rng + "&device=" + d2, []string{"s3"}},
		"unknown skill": {rng + "&skill=nope", []string{}},
		"range":         {"from=2026-11-02T00:00:00Z&to=2026-11-03T00:00:00Z", []string{"s2"}},
	} {
		var f stats.SessionPage
		x.get(t, "/api/v1/stats/sessions?"+c.q, &f)
		if !slices.Equal(ids(f), c.want) {
			t.Errorf("%s: %v want %v", name, ids(f), c.want)
		}
	}
}

func TestSessionIsLiveWhenRecentlyActive(t *testing.T) {
	x := newEnv(t)
	ts := time.Now().UTC().Add(-30 * time.Second).Format("2006-01-02T15:04:05.000Z")
	x.dt.Put(t, x.uid, x.dev, dt.File{Rel: "-work-alpha/live.jsonl", Lines: []string{dt.Prompt(ts, "now")}})
	x.dt.Derive(t, x.uid, 0)
	var pg stats.SessionPage
	x.get(t, "/api/v1/stats/sessions", &pg) // default range: last 30 days
	if len(pg.Items) != 1 || !pg.Items[0].IsLive {
		t.Fatalf("%+v", pg.Items)
	}
}

func TestSessionDetail(t *testing.T) {
	x := newEnv(t)
	seed(t, x)
	var det stats.SessionDetail
	if st := x.get(t, "/api/v1/stats/sessions/s1", &det); st != 200 {
		t.Fatalf("status %d", st)
	}
	agents := map[string]stats.AgentItem{}
	for _, a := range det.Agents {
		agents[a.AgentID] = a
	}
	m, a1 := agents["main"], agents["a1"]
	if len(det.Agents) != 2 || det.Agents[0].AgentID != "main" || m.ParentAgentID != nil || m.DurationMs != 3000 || m.DurationEstimated ||
		!reflect.DeepEqual(m.ToolCalls, map[string]int64{"Skill": 1, "Agent": 1, "Bash": 1}) || m.ToolErrors != 1 ||
		m.Usage.Messages != 2 || m.Usage.Tokens != 3300 || m.Usage.Input != 3000 || m.Usage.Output != 300 || !near(m.Usage.CostUSD, 0.018) {
		t.Fatalf("main %+v", m)
	}
	if a1.ParentAgentID == nil || *a1.ParentAgentID != "main" || a1.SpawnToolUseID == nil || *a1.SpawnToolUseID != "tuA" || a1.SpawnDepth != 1 ||
		a1.DurationMs != 5000 || a1.DurationEstimated || a1.Finished == nil || !*a1.Finished || a1.AgentType != "Explore" ||
		!reflect.DeepEqual(a1.ToolCalls, map[string]int64{"Read": 1}) || a1.Usage.Messages != 1 || a1.Usage.Tokens != 550 || !near(a1.Usage.CostUSD, 0.0015) {
		t.Fatalf("a1 %+v", a1)
	}
	if len(det.SkillPhases) != 1 || det.SkillPhases[0].AgentID != "main" || det.SkillPhases[0].Seq != 1 || det.SkillPhases[0].Skill != "superpowers:writing-plans" ||
		det.SkillPhases[0].EndedAt == nil || !det.SkillPhases[0].EndedAt.Equal(*m.EndedAt) {
		t.Fatalf("phases %+v", det.SkillPhases)
	}
	if len(det.Failures) != 2 || det.Failures[0].Category != "tool_error" || det.Failures[1].Category != "server_error" {
		t.Fatalf("failures %+v", det.Failures)
	}
	if det.Session.SessionID != "s1" || det.Session.Title != "Alpha work" {
		t.Fatalf("%+v", det.Session)
	}
	if st := x.get(t, "/api/v1/stats/sessions/ghost", nil); st != 404 {
		t.Fatalf("unknown session: %d", st)
	}
}

func TestSessionLines(t *testing.T) {
	x := newEnv(t)
	seed(t, x)
	typesOf := func(p stats.LinePage) []string {
		var out []string
		for _, l := range p.Lines {
			var o struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(l.Line, &o); err != nil {
				t.Fatalf("line is not JSON: %s", l.Line)
			}
			out = append(out, o.Type)
		}
		return out
	}
	var p stats.LinePage
	if st := x.get(t, "/api/v1/stats/sessions/s1/lines?agent=main&limit=3", &p); st != 200 || len(p.Lines) != 3 || p.Next == nil {
		t.Fatalf("%d %+v", st, p)
	}
	if got := typesOf(p); !slices.Equal(got, []string{"user", "assistant", "user"}) {
		t.Fatalf("%v", got)
	}
	all := typesOf(p)
	for p.Next != nil {
		next := *p.Next
		p = stats.LinePage{}
		x.get(t, "/api/v1/stats/sessions/s1/lines?agent=main&limit=3&after="+next, &p)
		all = append(all, typesOf(p)...)
	}
	if len(all) != 8 || all[6] != "ai-title" {
		t.Fatalf("main lines %v", all)
	}
	var sub stats.LinePage
	x.get(t, "/api/v1/stats/sessions/s1/lines?agent=a1", &sub)
	if len(sub.Lines) != 3 || sub.Next != nil {
		t.Fatalf("%+v", sub)
	}
	for _, path := range []string{"/api/v1/stats/sessions/s1/lines?agent=ghost", "/api/v1/stats/sessions/ghost/lines?agent=main"} {
		if st := x.get(t, path, nil); st != 404 {
			t.Errorf("%s: %d", path, st)
		}
	}
}

func TestLinePageQueryIsLimited(t *testing.T) {
	for _, order := range []string{"r.id", "r.id DESC"} {
		if q := stats.LinesQuery("r.id > $3", "true", order); !strings.HasSuffix(q, " LIMIT $4") {
			t.Errorf("unbounded line query for order %s: %s", order, q)
		}
	}
}

func TestOversizedLineIsAPlaceholder(t *testing.T) {
	x := newEnv(t)
	seed(t, x)
	x.dt.Exec(t, `UPDATE raw_lines SET line = convert_to(repeat('x', $1), 'UTF8') WHERE id = (SELECT min(id) FROM raw_lines)`, protocol.MaxReadLine+1)
	var p stats.LinePage
	if st := x.get(t, "/api/v1/stats/sessions/s1/lines?agent=main&limit=2", &p); st != 200 || len(p.Lines) != 2 {
		t.Fatalf("%d %+v", st, p)
	}
	var ph struct {
		TooLarge bool  `json:"too_large"`
		Offset   int64 `json:"offset"`
		Size     int64 `json:"size"`
	}
	if err := json.Unmarshal(p.Lines[0].Line, &ph); err != nil || !ph.TooLarge || ph.Size != protocol.MaxReadLine+1 || ph.Offset != 0 {
		t.Fatalf("%s %v", p.Lines[0].Line, err)
	}
}

// The page stops by the bytes it sends: invalid JSON is base64 encoded and grows by a third (R2).
func TestLinePageHasAByteBudget(t *testing.T) {
	x := newEnv(t)
	seed(t, x)
	x.dt.Exec(t, `UPDATE raw_lines SET line = convert_to(repeat('x', 3 * 1024 * 1024), 'UTF8') WHERE source_file_id = (SELECT source_file_id FROM derive_cursor WHERE session_id = 's1' AND agent_id = 'main' LIMIT 1)`)
	var p stats.LinePage
	if st := x.get(t, "/api/v1/stats/sessions/s1/lines?agent=main&limit=8", &p); st != 200 || len(p.Lines) != 2 || p.Next == nil {
		t.Fatalf("%d lines %d", st, len(p.Lines))
	}
}
