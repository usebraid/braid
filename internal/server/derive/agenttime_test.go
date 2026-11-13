// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package derive_test

import (
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"

	dt "github.com/usebraid/braid/internal/server/derive/derivetest"
)

// active derives one main transcript and returns "active_ms|turn_count|estimated" of its main agent.
func active(t *testing.T, batch int, lines ...string) string {
	t.Helper()
	e, u, d := setup(t)
	e.Put(t, u, d, dt.File{Rel: "-work-alpha/s1.jsonl", Lines: lines})
	e.Derive(t, u, batch)
	return e.Row(t, `SELECT duration_ms || '|' || turn_count || '|' || duration_estimated FROM agents WHERE agent_id='main'`)
}

func TestActiveTimeRules(t *testing.T) {
	u := dt.Usage(1, 1, 0, 0, 0)
	const h = 3600
	for name, tc := range map[string]struct {
		want  string
		lines []string
	}{
		"gaps up to 30 minutes count in full": {"1800000|1|false", []string{
			dt.Prompt(dt.At(0), "p"), dt.Reply(dt.At(600), "m1", opus, u), dt.Reply(dt.At(1800), "m2", opus, u)}},
		"a 5 h gap ending in an assistant line counts 0": {"60000|1|false", []string{
			dt.Prompt(dt.At(0), "p"), dt.Reply(dt.At(60), "m1", opus, u), dt.Reply(dt.At(60+5*h), "m2", opus, u)}},
		"a 2 h gap ending in a tool_result with durationMs counts 2 h": {"7210000|1|false", []string{
			dt.Prompt(dt.At(0), "p"), dt.Reply(dt.At(10), "m1", opus, u, dt.Tool("t1", "Bash", map[string]any{"command": "make"})),
			dt.Result(dt.At(10+2*h), "t1", false, map[string]any{"durationMs": 7200000})}},
		"the reported duration caps the gap": {"7210000|1|false", []string{
			dt.Prompt(dt.At(0), "p"), dt.Reply(dt.At(10), "m1", opus, u, dt.Tool("t1", "Bash", map[string]any{})),
			dt.Result(dt.At(10+3*h), "t1", false, map[string]any{"durationMs": 7200000})}},
		"an Agent result with totalDurationMs counts like durationMs": {"7210000|1|false", []string{
			dt.Prompt(dt.At(0), "p"), dt.Reply(dt.At(10), "m1", opus, u, dt.Tool("t1", "Agent", map[string]any{})),
			dt.Result(dt.At(10+2*h), "t1", false, map[string]any{"agentId": "a1", "totalDurationMs": 7200000})}},
		"a long tool_result without a reported duration counts 0": {"10000|1|false", []string{
			dt.Prompt(dt.At(0), "p"), dt.Reply(dt.At(10), "m1", opus, u, dt.Tool("t1", "Bash", map[string]any{})),
			dt.Result(dt.At(10+2*h), "t1", false, nil)}},
		"a turn with only a prompt counts 0":      {"0|0|false", []string{dt.Prompt(dt.At(0), "p")}},
		"a turn with only a tool_result counts 0": {"0|0|false", []string{dt.Prompt(dt.At(0), "p"), dt.Result(dt.At(10), "t1", false, nil)}},
		"a task notification starts a turn": {"20000|2|false", []string{
			dt.Prompt(dt.At(0), "p"), dt.Reply(dt.At(10), "m1", opus, u),
			dt.TaskNote(dt.At(4 * h)), dt.Reply(dt.At(4*h+10), "m2", opus, u)}},
		"isMeta user lines do not start a turn": {"10000|1|false", []string{
			dt.Prompt(dt.At(0), "p"), dt.Reply(dt.At(10), "m1", opus, u),
			dt.Line("user", dt.At(4*h), "isMeta", true, "message", map[string]any{"role": "user", "content": "x"}), dt.Reply(dt.At(4*h+10), "m2", opus, u)}},
		"turn_duration lines are ignored": {"10000|1|false", []string{
			dt.Prompt(dt.At(0), "p"), dt.Reply(dt.At(10), "m1", opus, u), dt.Turn(dt.At(11), 999999)}},
	} {
		for _, batch := range []int{0, 1} {
			if got := active(t, batch, tc.lines...); got != tc.want {
				t.Errorf("%s (batch %d): %s want %s", name, batch, got, tc.want)
			}
		}
	}
}

func TestActiveTimeOfSubagentsAndTotal(t *testing.T) {
	e, u, d := setup(t)
	e.Put(t, u, d,
		dt.File{Rel: "-work-alpha/s1.jsonl", Lines: []string{
			dt.Prompt(dt.At(0), "go"),
			dt.Reply(dt.At(10), "m1", opus, dt.Usage(1, 1, 0, 0, 0), dt.Tool("tuA", "Agent", map[string]any{})),
			dt.Result(dt.At(40), "tuA", false, map[string]any{"agentId": "a1", "totalDurationMs": 999999}), // not the source any more
		}},
		dt.File{Rel: "-work-alpha/s1/subagents/agent-a1.meta.json", JSON: `{"agentType":"Explore","toolUseId":"tuA","spawnDepth":1}`},
		dt.File{Rel: "-work-alpha/s1/subagents/agent-a1.jsonl", Lines: []string{
			dt.Side(dt.SidePrompt(dt.At(12), "task"), "a1"),
			dt.Side(dt.Reply(dt.At(20), "m2", opus, dt.Usage(1, 1, 0, 0, 0)), "a1")}},
		dt.File{Rel: "-work-alpha/s1/subagents/agent-a2.meta.json", JSON: `{"agentType":"Explore","toolUseId":"tuB","spawnDepth":1}`}) // no transcript at all
	e.Derive(t, u, 0)
	for agent, want := range map[string]string{"main": "40000|false", "a1": "8000|false", "a2": "0|true"} {
		if got := e.Row(t, `SELECT duration_ms || '|' || duration_estimated FROM agents WHERE agent_id=$1`, agent); got != want {
			t.Errorf("%s: %s want %s", agent, got, want)
		}
	}
}

func TestActiveTimeIsChunkIndependent(t *testing.T) {
	u := dt.Usage(1, 1, 0, 0, 0)
	rnd := rand.New(rand.NewPCG(3, 5)) //nolint:gosec // deterministic test data
	var lines []string
	at := 0
	for i := range 60 {
		switch rnd.IntN(4) {
		case 0:
			lines = append(lines, dt.Prompt(dt.At(at), "p"))
		case 1:
			lines = append(lines, dt.TaskNote(dt.At(at)))
		case 2:
			lines = append(lines, dt.Reply(dt.At(at), "m"+strconv.Itoa(i), opus, u, dt.Tool("t"+strconv.Itoa(i), "Bash", map[string]any{})))
		default:
			lines = append(lines, dt.Result(dt.At(at), "t"+strconv.Itoa(i-1), false, map[string]any{"durationMs": rnd.IntN(4_000_000)}))
		}
		at += []int{5, 90, 1700, 2000, 9000, 20000}[rnd.IntN(6)] // around the 30 minute threshold
	}
	want := active(t, 0, lines...)
	if strings.HasPrefix(want, "0|") {
		t.Fatalf("degenerate fixture %s", want)
	}
	for i := 0; i < 12; i++ {
		if got := active(t, 1+rnd.IntN(9), lines...); got != want {
			t.Fatalf("random batch size: %s want %s", got, want)
		}
	}
}

// A turn modelled on a real session: the 5-hour usage limit was hit and Claude Code waited 505 minutes for the reset.
func TestActiveTimeIgnoresALimitWait(t *testing.T) {
	u := dt.Usage(1, 1, 0, 0, 0)
	const wait = 505 * 60
	lines := []string{
		dt.Prompt(dt.At(0), "go"),
		dt.Reply(dt.At(30), "m1", opus, u, dt.Tool("t1", "Bash", map[string]any{})),
		dt.Result(dt.At(40), "t1", false, map[string]any{"durationMs": 9000}),
		dt.APIError(dt.At(50), "error", "rate_limit"),
		dt.Reply(dt.At(50+wait), "m2", opus, u),
		dt.Turn(dt.At(51+wait), (50+wait)*1000),
	}
	e, usr, d := setup(t)
	e.Put(t, usr, d, dt.File{Rel: "-work-alpha/s1.jsonl", Lines: lines})
	e.Derive(t, usr, 0)
	got, turn := e.N(t, `SELECT duration_ms FROM agents WHERE agent_id='main'`), (50+wait)*1000
	if got*10 >= turn || got != 50000 {
		t.Fatalf("active time %d ms, want 50000 and under 10%% of turn_duration (%d)", got, turn)
	}
}

func TestSkillPhases(t *testing.T) {
	e, u, d := setup(t)
	skill := func(id, name string) map[string]any { return dt.Tool(id, "Skill", map[string]any{"skill": name}) }
	e.Put(t, u, d,
		dt.File{Rel: "-work-alpha/s3.jsonl", Lines: []string{
			dt.Prompt(dt.At(0), "p"),
			dt.Reply(dt.At(10), "m1", opus, dt.Usage(1, 1, 0, 0, 0), skill("sk1", "superpowers:brainstorming")),
			dt.Reply(dt.At(20), "m2", opus, dt.Usage(1, 1, 0, 0, 0)),
			dt.Reply(dt.At(610), "m3", opus, dt.Usage(1, 1, 0, 0, 0), skill("sk2", "superpowers:writing-plans")),
			dt.Reply(dt.At(1000), "m4", opus, dt.Usage(1, 1, 0, 0, 0)),
		}},
		dt.File{Rel: "-work-alpha/s3/subagents/agent-a1.jsonl", Lines: []string{
			dt.Side(dt.Reply(dt.At(100), "m5", opus, dt.Usage(1, 1, 0, 0, 0), skill("sk3", "superpowers:debugging")), "a1"),
			dt.Side(dt.Reply(dt.At(150), "m6", opus, dt.Usage(1, 1, 0, 0, 0)), "a1"),
		}})
	e.Derive(t, u, 2)
	check := func(agent string, seq int, name string, start, end int) {
		t.Helper()
		if e.N(t, `SELECT count(*) FROM skill_phases WHERE session_id='s3' AND agent_id=$1 AND seq=$2 AND skill=$3
			AND started_at=$4::timestamptz AND ended_at=$5::timestamptz`, agent, seq, name, dt.At(start), dt.At(end)) != 1 {
			t.Errorf("missing phase %s/%d %s %d-%d; have %s", agent, seq, name, start, end,
				e.Row(t, `SELECT string_agg(agent_id||'/'||seq||'/'||skill||'/'||started_at::text||'/'||coalesce(ended_at::text,'-'), ' ; ') FROM skill_phases`))
		}
	}
	check("main", 1, "superpowers:brainstorming", 10, 610)   // lasts until the next Skill call of the same agent
	check("main", 2, "superpowers:writing-plans", 610, 1000) // the last one lasts until the agent's end
	check("a1", 1, "superpowers:debugging", 100, 150)
	if e.N(t, `SELECT count(*) FROM skill_phases`) != 3 {
		t.Fatal("unexpected phases")
	}
	e.Derive(t, u, 2) // idempotent
	if e.N(t, `SELECT count(*) FROM skill_phases`) != 3 {
		t.Fatal("phases duplicated")
	}
}

func TestAgentParentsFromToolCallsHintsAndDefault(t *testing.T) {
	e, u, d := setup(t)
	meta := func(id, body string) dt.File {
		return dt.File{Rel: "-work-alpha/s4/subagents/agent-" + id + ".meta.json", JSON: body}
	}
	e.Put(t, u, d,
		meta("a2", `{"toolUseId":"tuB","spawnDepth":2}`), // listed first on purpose: links must not depend on arrival order
		meta("a4", `{"toolUseId":"tuX","parentAgentId":"a1","spawnDepth":2}`),
		meta("a5", `{"toolUseId":"tuY","spawnDepth":1}`),
		meta("a1", `{"toolUseId":"tuA","spawnDepth":1}`),
		dt.File{Rel: "-work-alpha/s4/subagents/agent-a1.jsonl", Lines: []string{
			dt.Side(dt.Reply(dt.At(2), "m2", opus, dt.Usage(1, 1, 0, 0, 0), dt.Tool("tuB", "Agent", map[string]any{})), "a1")}},
		dt.File{Rel: "-work-alpha/s4.jsonl", Lines: []string{
			dt.Reply(dt.At(1), "m1", opus, dt.Usage(1, 1, 0, 0, 0), dt.Tool("tuA", "Agent", map[string]any{}))}})
	e.Derive(t, u, 0)
	got := e.Row(t, `SELECT string_agg(agent_id || '>' || coalesce(parent_agent_id, '-'), ',' ORDER BY agent_id) FROM agents WHERE session_id='s4'`)
	if got != "a1>main,a2>a1,a4>a1,a5>main,main>-" {
		t.Fatalf("%s", got)
	}
}

func TestDerivationIsIndependentOfArrivalOrder(t *testing.T) {
	e := dt.New(t)
	u1, u2 := e.User(t, "u1"), e.User(t, "u2")
	d1, d2 := e.Device(t, u1, "laptop"), e.Device(t, u2, "laptop")
	files := []dt.File{
		{Rel: "-work-alpha/s1.jsonl", Lines: []string{
			dt.Prompt(dt.At(0), "go"),
			dt.Reply(dt.At(1), "m1", opus, dt.Usage(10, 5, 0, 0, 0), dt.Tool("tuA", "Agent", map[string]any{}), dt.Tool("sk1", "Skill", map[string]any{"skill": "x:y"})),
			dt.Result(dt.At(3), "tuA", false, map[string]any{"agentId": "a1", "status": "completed", "totalDurationMs": 7000}),
			dt.Turn(dt.At(60), 1000)}},
		{Rel: "-work-alpha/s1/subagents/agent-a1.meta.json", JSON: `{"agentType":"Explore","toolUseId":"tuA","spawnDepth":1}`},
		{Rel: "-work-alpha/s1/subagents/agent-a1.jsonl", Lines: []string{
			dt.Side(dt.SidePrompt(dt.At(10), "t"), "a1"), dt.Side(dt.Reply(dt.At(20), "m2", opus, dt.Usage(3, 3, 0, 0, 0)), "a1")}},
		{Rel: "-work-alpha/s1/custom-title.json", JSON: `{"customTitle":"T"}`},
	}
	e.Put(t, u1, d1, files...)
	for i := len(files) - 1; i >= 0; i-- {
		e.Put(t, u2, d2, files[i])
	}
	e.Derive(t, u1, 0)
	e.Derive(t, u2, 2)
	repl := map[string]string{u1: "U", u2: "U", d1: "D", d2: "D"}
	if a, b := e.Dump(t, u1, repl), e.Dump(t, u2, repl); a != b || len(a) < 200 {
		t.Fatalf("dumps differ:\n%s\n----\n%s", a, b)
	}
}
