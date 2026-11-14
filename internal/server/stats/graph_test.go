// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package stats_test

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/usebraid/braid/internal/server/api"
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
	var s1 stats.ChatPage // a line of another session is ignored: the first page
	if st := x.get(t, "/api/v1/stats/sessions/s1/chat?at="+itoa(sub.ID), &s1); st != 200 || len(s1.Lines) == 0 || s1.Prev != nil || s1.Lines[0].AgentID != "main" {
		t.Fatalf("line of another session: %d %+v", st, s1)
	}
	if st := x.get(t, "/api/v1/stats/sessions/p1/chat?at=999999999", &pg); st != 200 || !slices.Equal(chatIDs(pg.Lines), chatIDs(all.Lines)) {
		t.Fatalf("unknown line: %d %v", st, chatIDs(pg.Lines))
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

func TestChatLatest(t *testing.T) {
	x := newEnv(t)
	d1, d2 := seed(t, x)
	seedParallel(t, x, d1, d2)
	var all, pg, up stats.ChatPage
	x.get(t, "/api/v1/stats/sessions/p1/chat", &all)
	if st := x.get(t, "/api/v1/stats/sessions/p1/chat?latest=1&limit=3", &pg); st != 200 || !slices.Equal(chatIDs(pg.Lines), chatIDs(all.Lines[5:])) || pg.Next != nil || pg.Prev == nil {
		t.Fatalf("%d %v %+v", st, chatIDs(pg.Lines), pg)
	}
	x.get(t, "/api/v1/stats/sessions/p1/chat?limit=3&before="+url.QueryEscape(*pg.Prev), &up)
	if !slices.Equal(chatIDs(up.Lines), chatIDs(all.Lines[2:5])) {
		t.Fatalf("before: %v", chatIDs(up.Lines))
	}
	if x.get(t, "/api/v1/stats/sessions/p1/chat?latest=1", &pg); !slices.Equal(chatIDs(pg.Lines), chatIDs(all.Lines)) || pg.Next != nil || pg.Prev != nil {
		t.Fatalf("whole session: %+v", pg)
	}
	if x.get(t, "/api/v1/stats/sessions/p1/chat?latest=2", nil) != 400 || x.get(t, "/api/v1/stats/sessions/p1/chat?latest=1&at=1", nil) != 400 {
		t.Fatal("status")
	}
}

func TestTimelineEventIDsUnique(t *testing.T) {
	x := newEnv(t)
	d1, d2 := seed(t, x)
	seedParallel(t, x, d1, d2)
	var pg stats.TimelinePage
	x.get(t, "/api/v1/stats/timeline?"+rng, &pg)
	seen := map[string]bool{}
	for _, e := range pg.Events {
		if e.ID == "" || seen[e.ID] {
			t.Fatalf("id %q of %s/%s", e.ID, e.SessionID, e.Kind)
		}
		seen[e.ID] = true
	}
	if len(seen) != 8 {
		t.Fatalf("%d events", len(seen))
	}
}

func TestBackgroundAgentMergesAtTaskNote(t *testing.T) {
	x := newEnv(t)
	bg := map[string]any{"description": "bg", "run_in_background": true}
	x.dt.Put(t, x.uid, x.dev,
		dt.File{Rel: "-work-bg/g1.jsonl", Lines: []string{
			dt.Prompt(dt.At(day(5)), "go"),
			dt.Reply(dt.At(day(5)+10), "g-m1", opus, dt.Usage(1, 1, 0, 0, 0), dt.Tool("tuG1", "Agent", bg), dt.Tool("tuG2", "Agent", bg)),
			dt.Result(dt.At(day(5)+11), "tuG1", false, map[string]any{"status": "async_launched"}),
			dt.Result(dt.At(day(5)+11), "tuG2", false, map[string]any{"status": "async_launched"}),
			dt.Notify(dt.At(day(5)+100), "tuG1", 90000),
			dt.Reply(dt.At(day(5)+110), "g-m2", opus, dt.Usage(1, 1, 0, 0, 0), dt.Text("done")),
		}},
		dt.File{Rel: "-work-bg/g1/subagents/agent-g1.meta.json", JSON: `{"agentType":"Explore","description":"one","toolUseId":"tuG1","spawnDepth":1}`},
		dt.File{Rel: "-work-bg/g1/subagents/agent-g1.jsonl", Lines: []string{dt.Side(dt.SidePrompt(dt.At(day(5)+12), "s"), "g1")}},
		dt.File{Rel: "-work-bg/g1/subagents/agent-g2.meta.json", JSON: `{"agentType":"Explore","description":"two","toolUseId":"tuG2","spawnDepth":1}`},
		dt.File{Rel: "-work-bg/g1/subagents/agent-g2.jsonl", Lines: []string{dt.Side(dt.SidePrompt(dt.At(day(5)+12), "s"), "g2")}})
	x.dt.Derive(t, x.uid, 0)
	at := func(sec int) time.Time { v, _ := time.Parse("2006-01-02T15:04:05.000Z", dt.At(sec)); return v }
	var det stats.SessionDetail
	x.get(t, "/api/v1/stats/sessions/g1", &det)
	by := map[string]stats.AgentItem{}
	for _, a := range det.Agents {
		by[a.AgentID] = a
	}
	if a := by["g1"]; !a.Background || a.MergedAt == nil || !a.MergedAt.Equal(at(day(5)+100)) {
		t.Fatalf("g1 merges at its task note: %+v", a)
	}
	if a := by["g2"]; !a.Background || a.MergedAt != nil { // launched, no note yet: still open
		t.Fatalf("g2: %+v", a)
	}
	x.dt.Exec(t, `UPDATE tool_calls SET note_at = NULL WHERE tool_use_id = 'tuG1'`) // a note line without a time
	x.get(t, "/api/v1/stats/sessions/g1", &det)
	for _, a := range det.Agents {
		if a.AgentID == "g1" && (a.MergedAt == nil || !a.MergedAt.Equal(at(day(5)+100))) { // spawn + 90 s
			t.Fatalf("fallback: %+v", a)
		}
	}
}

func TestSameSessionIDTwoUsers(t *testing.T) {
	x := newEnv(t)
	cB := x.Login(t, "u2")
	var meB api.Me
	x.getAs(t, cB, "/api/v1/me", &meB)
	dB := x.dt.Device(t, meB.ID, "b-laptop")
	file := func(text string, sec int) dt.File {
		return dt.File{Rel: "-work-same/shared-id.jsonl", Lines: []string{dt.Prompt(dt.At(day(5)+sec), text), dt.Reply(dt.At(day(5)+sec+5), "m-"+text, opus, dt.Usage(1, 1, 0, 0, 0), dt.Text("re "+text))}}
	}
	x.dt.Put(t, x.uid, x.dev, file("from-a", 0))
	x.dt.Put(t, meB.ID, dB, file("from-b", 600))
	x.dt.Derive(t, x.uid, 0)
	x.dt.Derive(t, meB.ID, 0)
	for _, c := range []struct {
		c            *http.Client
		mine, theirs string
		at           int
	}{{x.c, "from-a", "from-b", 0}, {cB, "from-b", "from-a", 600}} {
		var tp stats.TimelinePage
		if x.getAs(t, c.c, "/api/v1/stats/timeline?"+rng, &tp); len(tp.Events) != 1 || tp.Events[0].SessionID != "shared-id" ||
			!tp.Events[0].At.Equal(time.Unix(dt.Unix(day(5)+c.at), 0)) {
			t.Fatalf("%s timeline: %+v", c.mine, tp.Events)
		}
		var pg stats.ChatPage
		x.getAs(t, c.c, "/api/v1/stats/sessions/shared-id/chat", &pg)
		var raw []byte
		for _, l := range pg.Lines {
			raw = append(raw, l.Line...)
		}
		if len(pg.Lines) != 2 || !strings.Contains(string(raw), c.mine) || strings.Contains(string(raw), c.theirs) {
			t.Fatalf("%s chat: %s", c.mine, raw)
		}
	}
}
