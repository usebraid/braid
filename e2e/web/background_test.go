// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

//go:build webe2e

package web

import (
	dt "github.com/usebraid/braid/internal/server/derive/derivetest"
)

// bgSession is a small session "bg1" that shows every dot type of the session graph: a background agent
// (reports back by task notification), a foreground agent, a background Bash call with its notification, a failed
// tool call, a skill call, a compaction and an interrupt. It sits two days before the other fixtures.
func bgSession() []dt.File {
	const rel = "-work-delta/bg1"
	delta := func(l string) string { return dt.Cwd(l, "/work/delta") }
	t := -150000
	at := func(s int) string { return dt.At(t + s) }
	u := dt.Usage(100, 10, 0, 0, 0)
	return []dt.File{
		{Rel: rel + ".jsonl", Lines: []string{
			delta(dt.Prompt(at(0), "Background fixture prompt")),
			delta(dt.Reply(at(5), "bg-m1", opus, u, dt.Text("starting helpers"),
				dt.Tool("bgA", "Agent", map[string]any{"description": "background research"}),
				dt.Tool("fgA", "Agent", map[string]any{"description": "foreground helper"}),
				dt.Tool("bgC", "Bash", map[string]any{"command": "sleep 300", "run_in_background": true}))),
			delta(dt.Result(at(6), "bgA", false, map[string]any{"agentId": "bga", "status": "async_launched"})),
			delta(dt.Result(at(7), "bgC", false, map[string]any{"backgroundTaskId": "bgc1"})),
			delta(dt.Result(at(40), "fgA", false, map[string]any{"agentId": "fga", "status": "completed", "totalDurationMs": 30000})),
			delta(dt.Reply(at(50), "bg-m2", opus, u, dt.Tool("tfail", "Bash", map[string]any{"command": "go vet ./..."}))),
			delta(dt.Result(at(52), "tfail", true, nil)),
			delta(dt.Reply(at(60), "bg-m3", opus, u, dt.Tool("tskill", "Skill", map[string]any{"skill": "superpowers:brainstorming"}))),
			delta(dt.Result(at(61), "tskill", false, nil)),
			delta(dt.Line("system", at(100), "subtype", "compact_boundary")),
			delta(dt.Line("user", at(120), "message", map[string]any{"role": "user",
				"content": []any{map[string]any{"type": "text", "text": "[Request interrupted by user]"}}})),
			delta(dt.Notify(at(200), "bgC", 190000)),
			delta(dt.Notify(at(240), "bgA", 230000)),
			delta(dt.Reply(at(250), "bg-m4", opus, u, dt.Text("background fixture done"))),
		}},
		{Rel: rel + "/subagents/agent-bga.meta.json", JSON: `{"agentType":"general-purpose","description":"background research","toolUseId":"bgA","spawnDepth":1,"finished":true}`},
		{Rel: rel + "/subagents/agent-bga.jsonl", Lines: []string{
			dt.Side(delta(dt.SidePrompt(at(8), "research brief")), "bga"),
			dt.Side(delta(dt.Reply(at(80), "bg-s1", opus, u, dt.Text("researching in background"))), "bga"),
			dt.Side(delta(dt.Reply(at(230), "bg-s2", opus, u, dt.Text("background findings"))), "bga"),
		}},
		{Rel: rel + "/subagents/agent-fga.meta.json", JSON: `{"agentType":"Explore","description":"foreground helper","toolUseId":"fgA","spawnDepth":1,"finished":true}`},
		{Rel: rel + "/subagents/agent-fga.jsonl", Lines: []string{
			dt.Side(delta(dt.SidePrompt(at(9), "foreground brief")), "fga"),
			dt.Side(delta(dt.Reply(at(30), "bg-s3", opus, u, dt.Text("foreground findings"))), "fga"),
		}},
	}
}
