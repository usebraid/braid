// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package stats_test

import (
	"net/url"
	"slices"
	"strconv"
	"testing"
	"time"

	dt "github.com/usebraid/braid/internal/server/derive/derivetest"
	"github.com/usebraid/braid/internal/server/stats"
)

// seedParallel adds session p1: main spawns b1 (finished) and b2 (still running) whose lines share timestamps; and
// q1/q2, two sessions with a start at the same instant, plus r1 started on d1 and resumed on d2 after 45 minutes.
func seedParallel(t *testing.T, x *env, d1, d2 string) {
	t.Helper()
	x.dt.Put(t, x.uid, d1,
		dt.File{Rel: "-work-par/p1.jsonl", Lines: []string{
			dt.Prompt(dt.At(day(5)), "go"),
			dt.Reply(dt.At(day(5)+10), "p-m1", opus, dt.Usage(1, 1, 0, 0, 0), dt.Tool("tuB1", "Agent", map[string]any{}), dt.Tool("tuB2", "Agent", map[string]any{})),
			dt.Result(dt.At(day(5)+20), "tuB1", false, map[string]any{"agentId": "b1", "status": "completed", "totalDurationMs": 5000}),
			dt.Reply(dt.At(day(5)+30), "p-m2", opus, dt.Usage(1, 1, 0, 0, 0), dt.Text("done")),
		}},
		dt.File{Rel: "-work-par/p1/subagents/agent-b1.meta.json", JSON: `{"agentType":"Explore","description":"one","toolUseId":"tuB1","spawnDepth":1,"finished":true}`},
		dt.File{Rel: "-work-par/p1/subagents/agent-b1.jsonl", Lines: []string{
			dt.Side(dt.SidePrompt(dt.At(day(5)+11), "s"), "b1"),
			dt.Side(dt.Reply(dt.At(day(5)+12), "p-m3", sonnet, dt.Usage(1, 1, 0, 0, 0), dt.Text("b1")), "b1"),
		}},
		dt.File{Rel: "-work-par/p1/subagents/agent-b2.meta.json", JSON: `{"agentType":"Explore","description":"two","toolUseId":"tuB2","spawnDepth":1}`},
		dt.File{Rel: "-work-par/p1/subagents/agent-b2.jsonl", Lines: []string{
			dt.Side(dt.SidePrompt(dt.At(day(5)+11), "s"), "b2"),
			dt.Side(dt.Reply(dt.At(day(5)+12), "p-m4", sonnet, dt.Usage(1, 1, 0, 0, 0), dt.Text("b2")), "b2"),
		}},
		dt.File{Rel: "-work-par/q1.jsonl", Lines: []string{dt.Prompt(dt.At(day(6)), "q1"), dt.Reply(dt.At(day(6)+5), "q-m1", opus, dt.Usage(1, 1, 0, 0, 0), dt.Text("x"))}},
		dt.File{Rel: "-work-par/q2.jsonl", Lines: []string{dt.Prompt(dt.At(day(6)), "q2"), dt.Reply(dt.At(day(6)+5), "q-m2", opus, dt.Usage(1, 1, 0, 0, 0), dt.Text("x"))}},
		dt.File{Rel: "-work-par/r1.jsonl", Lines: []string{dt.Prompt(dt.At(day(7)), "r1"), dt.Reply(dt.At(day(7)+5), "r-m1", opus, dt.Usage(1, 1, 0, 0, 0), dt.Text("x"))}})
	x.dt.Put(t, x.uid, d2,
		dt.File{Rel: "-work-par/r1.jsonl", Lines: []string{dt.Prompt(dt.At(day(7)+45*60), "r1 again"), dt.Reply(dt.At(day(7)+45*60+5), "r-m2", sonnet, dt.Usage(1, 1, 0, 0, 0), dt.Text("y"))}})
	x.dt.Derive(t, x.uid, 0)
}

func eventKeys(es []stats.TimelineEvent) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.SessionID+"/"+e.Kind)
	}
	return out
}

func TestTimelinePagingStable(t *testing.T) {
	x := newEnv(t)
	d1, d2 := seed(t, x)
	seedParallel(t, x, d1, d2)
	var all stats.TimelinePage
	if st := x.get(t, "/api/v1/stats/timeline?"+rng, &all); st != 200 || all.Next != nil {
		t.Fatalf("%d %+v", st, all)
	}
	want := []string{"r1/resume", "r1/start", "q2/start", "q1/start", "p1/start", "s3/start", "s2/start", "s1/start"}
	if !slices.Equal(eventKeys(all.Events), want) {
		t.Fatalf("%v", eventKeys(all.Events))
	}
	var got []string
	cur := ""
	for i := 0; i < 20; i++ { // limit 1 puts q1 and q2, which share `at`, on a page border
		var pg stats.TimelinePage
		x.get(t, "/api/v1/stats/timeline?"+rng+"&limit=1"+cur, &pg)
		got = append(got, eventKeys(pg.Events)...)
		if pg.Next == nil {
			break
		}
		cur = "&cursor=" + url.QueryEscape(*pg.Next)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("paged: %v", got)
	}
	if e := all.Events[7]; e.Title != "Alpha work" || e.ProjectName != "alpha" || e.Failures != 2 || e.IsLive || e.Model == nil || *e.Model != opus {
		t.Fatalf("%+v", e)
	}
	if st := x.get(t, "/api/v1/stats/timeline?"+rng+"&cursor=bad", nil); st != 400 {
		t.Fatalf("bad cursor: %d", st)
	}
}

func TestTimelineDeviceFilterMatchesEvent(t *testing.T) {
	x := newEnv(t)
	d1, d2 := seed(t, x)
	seedParallel(t, x, d1, d2)
	var pg stats.TimelinePage
	x.get(t, "/api/v1/stats/timeline?"+rng+"&device="+d2, &pg)
	if want := []string{"r1/resume", "s3/start"}; !slices.Equal(eventKeys(pg.Events), want) {
		t.Fatalf("%v", eventKeys(pg.Events))
	}
	x.get(t, "/api/v1/stats/timeline?"+rng+"&device="+d1+"&model="+sonnet, &pg)
	if len(pg.Events) != 0 {
		t.Fatalf("%v", eventKeys(pg.Events))
	}
}

func chatIDs(ls []stats.ChatLine) []int64 {
	var out []int64
	for _, l := range ls {
		out = append(out, l.ID)
	}
	return out
}

func TestChatInterleavesAgents(t *testing.T) {
	x := newEnv(t)
	d1, d2 := seed(t, x)
	seedParallel(t, x, d1, d2)
	var all stats.ChatPage
	if st := x.get(t, "/api/v1/stats/sessions/p1/chat", &all); st != 200 || all.Next != nil || all.Prev != nil {
		t.Fatalf("%d %+v", st, all)
	}
	var agents []string
	for _, l := range all.Lines {
		agents = append(agents, l.AgentID)
	}
	if want := []string{"main", "main", "b1", "b2", "b1", "b2", "main", "main"}; !slices.Equal(agents, want) {
		t.Fatalf("%v", agents)
	}
	var got []stats.ChatLine
	cur := ""
	for i := 0; i < 20; i++ { // limit 3 puts the border inside the equal timestamps
		var pg stats.ChatPage
		x.get(t, "/api/v1/stats/sessions/p1/chat?limit=3"+cur, &pg)
		got = append(got, pg.Lines...)
		if pg.Next == nil {
			break
		}
		cur = "&after=" + url.QueryEscape(*pg.Next)
	}
	if !slices.Equal(chatIDs(got), chatIDs(all.Lines)) {
		t.Fatalf("paged %v want %v", chatIDs(got), chatIDs(all.Lines))
	}
	var back stats.ChatPage // the newest 3 via before
	x.get(t, "/api/v1/stats/sessions/p1/chat?limit=3&at="+itoa(all.Lines[5].ID), &back)
	var up stats.ChatPage
	x.get(t, "/api/v1/stats/sessions/p1/chat?limit=3&before="+url.QueryEscape(*back.Prev), &up)
	if !slices.Equal(chatIDs(up.Lines), chatIDs(all.Lines[2:5])) || up.Prev == nil || up.Next == nil {
		t.Fatalf("before: %v", chatIDs(up.Lines))
	}
	if x.get(t, "/api/v1/stats/sessions/nope/chat", nil) != 404 || x.get(t, "/api/v1/stats/sessions/p1/chat?after=x", nil) != 400 ||
		x.get(t, "/api/v1/stats/sessions/p1/chat?at=1&after="+url.QueryEscape(*back.Prev), nil) != 400 {
		t.Fatal("status")
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestChatAtLine(t *testing.T) {
	x := newEnv(t)
	d1, d2 := seed(t, x)
	seedParallel(t, x, d1, d2)
	var all, pg stats.ChatPage
	x.get(t, "/api/v1/stats/sessions/p1/chat", &all)
	sub := all.Lines[4] // b1's reply
	if st := x.get(t, "/api/v1/stats/sessions/p1/chat?at="+itoa(sub.ID), &pg); st != 200 || pg.Lines[0].ID != sub.ID || pg.Prev == nil || len(pg.Lines) != 4 {
		t.Fatalf("%d %v", st, chatIDs(pg.Lines))
	}
	if x.get(t, "/api/v1/stats/sessions/s1/chat?at="+itoa(sub.ID), nil) != 404 {
		t.Fatal("line of another session")
	}
}

func TestChatLineWithoutTimestampSortsFirst(t *testing.T) {
	x := newEnv(t)
	d1, d2 := seed(t, x)
	seedParallel(t, x, d1, d2)
	x.dt.Exec(t, `UPDATE line_order SET ts = '-infinity' WHERE raw_line_id = (SELECT max(raw_line_id) FROM line_order WHERE session_id = 'p1')`)
	var pg stats.ChatPage
	x.get(t, "/api/v1/stats/sessions/p1/chat?limit=2", &pg)
	if pg.Lines[0].Ts != nil || pg.Lines[1].Ts == nil || pg.Next == nil {
		t.Fatalf("%+v", pg)
	}
	var nx, pv stats.ChatPage
	x.get(t, "/api/v1/stats/sessions/p1/chat?limit=2&after="+url.QueryEscape(*pg.Next), &nx)
	x.get(t, "/api/v1/stats/sessions/p1/chat?limit=2&before="+url.QueryEscape(*nx.Prev), &pv)
	if len(nx.Lines) != 2 || nx.Prev == nil || !slices.Equal(chatIDs(pv.Lines), chatIDs(pg.Lines)) {
		t.Fatalf("%v %v", chatIDs(nx.Lines), chatIDs(pv.Lines))
	}
}

func TestAgentSpawnMerge(t *testing.T) {
	x := newEnv(t)
	d1, d2 := seed(t, x)
	seedParallel(t, x, d1, d2)
	at := func(sec int) *time.Time { v, _ := time.Parse("2006-01-02T15:04:05.000Z", dt.At(sec)); return &v }
	var det stats.SessionDetail
	x.get(t, "/api/v1/stats/sessions/p1", &det)
	by := map[string]stats.AgentItem{}
	for _, a := range det.Agents {
		by[a.AgentID] = a
	}
	if a := by["b1"]; a.SpawnedAt == nil || !a.SpawnedAt.Equal(*at(day(5) + 10)) || a.MergedAt == nil || !a.MergedAt.Equal(*at(day(5) + 20)) {
		t.Fatalf("b1: %+v", a)
	}
	if a := by["b2"]; a.SpawnedAt == nil || !a.SpawnedAt.Equal(*at(day(5) + 10)) || a.MergedAt != nil {
		t.Fatalf("b2: %+v", a)
	}
	if a := by["main"]; a.SpawnedAt != nil || a.MergedAt != nil {
		t.Fatalf("main: %+v", a)
	}
}
