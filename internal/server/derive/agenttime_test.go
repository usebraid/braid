// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package derive_test

import (
	"strconv"
	"testing"

	dt "github.com/usebraid/braid/internal/server/derive/derivetest"
)

func TestAgentTimeSources(t *testing.T) {
	e, u, d := setup(t)
	meta := func(id, extra string) dt.File {
		return dt.File{Rel: "-work-alpha/s1/subagents/agent-" + id + ".meta.json", JSON: `{"agentType":"Explore","description":"x",` + extra + `}`}
	}
	sub := func(id string, secs ...int) dt.File {
		f := dt.File{Rel: "-work-alpha/s1/subagents/agent-" + id + ".jsonl"}
		for i, s := range secs {
			if i == 0 {
				f.Lines = append(f.Lines, dt.Side(dt.SidePrompt(dt.At(s), "task"), id))
			} else {
				f.Lines = append(f.Lines, dt.Side(dt.Reply(dt.At(s), id+"-m"+strconv.Itoa(i), opus, dt.Usage(1, 1, 0, 0, 0)), id))
			}
		}
		return f
	}
	e.Put(t, u, d,
		dt.File{Rel: "-work-alpha/s1.jsonl", Lines: []string{
			dt.Prompt(dt.At(0), "go"),
			dt.Reply(dt.At(1), "m1", opus, dt.Usage(1, 1, 0, 0, 0), dt.Tool("tuA", "Agent", map[string]any{"description": "sync worker"})),
			dt.Reply(dt.At(2), "m2", opus, dt.Usage(1, 1, 0, 0, 0), dt.Tool("tuB", "Agent", map[string]any{"description": "async worker"})),
			dt.Result(dt.At(3), "tuA", false, map[string]any{"agentId": "a1", "status": "completed", "totalDurationMs": 7000}),
			dt.Result(dt.At(4), "tuB", false, map[string]any{"agentId": "a2", "status": "async_launched"}),
			dt.Notify(dt.At(50), "tuB", 4200),
			dt.Turn(dt.At(60), 1000),
			dt.Turn(dt.At(70), 2500),
		}},
		meta("a1", `"toolUseId":"tuA","spawnDepth":1`), sub("a1", 10, 20),
		meta("a2", `"toolUseId":"tuB","spawnDepth":1`), sub("a2", 12, 30),
		meta("a3", `"toolUseId":"tuC","spawnDepth":1`), sub("a3", 100, 130, 1300)) // no measured value anywhere: capped-gap estimate
	e.Derive(t, u, 0)
	for agent, want := range map[string]string{"main": "3500|false", "a1": "7000|false", "a2": "4200|false", "a3": "330000|true"} {
		if got := e.Row(t, `SELECT duration_ms, duration_estimated FROM agents WHERE agent_id=$1`, agent); got != want {
			t.Errorf("%s: %s want %s", agent, got, want)
		}
	}
}

func TestMainAgentFallbackEstimateIsChunkIndependent(t *testing.T) {
	e, u, d := setup(t)
	e.Put(t, u, d, dt.File{Rel: "-work-alpha/s2.jsonl", Lines: []string{ // an older Claude Code without turn_duration lines
		dt.Prompt(dt.At(0), "p"),
		dt.Reply(dt.At(30), "m1", opus, dt.Usage(1, 1, 0, 0, 0)),
		dt.Reply(dt.At(1000), "m2", opus, dt.Usage(1, 1, 0, 0, 0)), // 970 s gap, capped at 300 s
	}})
	e.Derive(t, u, 1) // one row per transaction: the gap logic must survive chunk borders
	if got := e.Row(t, `SELECT duration_ms, duration_estimated FROM agents WHERE session_id='s2' AND agent_id='main'`); got != "330000|true" {
		t.Fatalf("%s", got)
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
