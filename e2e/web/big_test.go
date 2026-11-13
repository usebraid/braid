// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

//go:build webe2e

package web

import (
	"fmt"

	dt "github.com/usebraid/braid/internal/server/derive/derivetest"
)

var bigTypes = []string{"Explore", "general-purpose", "Plan", "code-reviewer", "implementer", "test-runner"}

var bigTasks = []string{
	"Map the ingest path and list every writer", "Review the chunked upload for races", "Draft the release checklist",
	"Find callers of the session deriver", "Write tests for the quota pause", "Check the CSP against the built assets",
	"Trace a rate limit through the daemon", "Audit device token rotation", "Port the board palette to tokens",
}

// bigSession is a synthetic session of 102 subagents in three levels over about 15 hours, the day before the
// other fixtures: main spawns 18 agents (more than the canvas groups), six of them spawn 8 each, and twelve
// of those spawn 3 each. It has no failures, so the failure list and counts of the other specs stay as they are.
func bigSession() []dt.File {
	const rel = "-work-gamma/big1"
	gamma := func(l string) string { return dt.Cwd(l, "/work/gamma") }
	start := -70000 // seconds from the derivetest base; 2026-10-31
	var files []dt.File
	n := 0
	// spawn writes agent id's subagent files and returns the lines its parent writes: the Agent call and its result.
	var spawn func(parent string, depth, at, kids, grand int) (call map[string]any, result string, end int)
	spawn = func(parent string, depth, at, kids, grand int) (map[string]any, string, int) {
		n++
		id := fmt.Sprintf("b%03d", n)
		tu := "tu-" + id
		typ, task := bigTypes[n%len(bigTypes)], bigTasks[n%len(bigTasks)]
		lines := []string{dt.Side(gamma(dt.SidePrompt(dt.At(at), task)), id)}
		t := at + 30
		var calls []map[string]any
		var results []string
		for k := 0; k < kids; k++ {
			g := 0
			if k < 2 {
				g = grand
			}
			c, r, e := spawn(id, depth+1, t+k*20, g, 0)
			calls = append(calls, c)
			results = append(results, dt.Side(gamma(r), id))
			t = max(t, e)
		}
		model := "claude-sonnet-5-5"
		if depth == 1 {
			model = opus
		}
		lines = append(lines, dt.Side(gamma(dt.Reply(dt.At(at+20), "m-"+id, model, dt.Usage(4000+n*37, 600+n*11, 20000, 0, 0), calls...)), id))
		lines = append(lines, results...)
		end := t + 240 + (n%7)*180
		lines = append(lines, dt.Side(gamma(dt.Reply(dt.At(end), "m2-"+id, model, dt.Usage(800, 300, 9000, 0, 0), dt.Text("done"))), id))
		files = append(files,
			dt.File{Rel: fmt.Sprintf("%s/subagents/agent-%s.meta.json", rel, id), JSON: fmt.Sprintf(
				`{"agentType":%q,"description":%q,"toolUseId":%q,"parentAgentId":%q,"spawnDepth":%d,"finished":true}`, typ, task, tu, parent, depth)},
			dt.File{Rel: fmt.Sprintf("%s/subagents/agent-%s.jsonl", rel, id), Lines: lines})
		call := dt.Tool(tu, "Agent", map[string]any{"description": task, "subagent_type": typ})
		res := dt.Result(dt.At(end), tu, false, map[string]any{"agentId": id, "status": "completed", "totalDurationMs": (end - at) * 1000})
		return call, res, end
	}
	main := []string{gamma(dt.Prompt(dt.At(start), "Ship the release"))}
	t := start + 60
	for k := 0; k < 18; k++ {
		kids, grand := 0, 0
		if k%3 == 0 {
			kids = 8
			grand = 3
		}
		c, r, e := spawn("main", 1, t, kids, grand)
		main = append(main, gamma(dt.Reply(dt.At(t), fmt.Sprintf("big-m%d", k), opus, dt.Usage(9000, 900, 60000, 0, 0), dt.Text("next"), c)), gamma(r))
		t = e + 300
	}
	main = append(main, gamma(dt.Line("ai-title", "", "aiTitle", "Release run with 102 subagents")))
	return append([]dt.File{{Rel: rel + ".jsonl", Lines: main}}, files...)
}
