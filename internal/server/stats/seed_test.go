// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package stats_test

import (
	"testing"

	dt "github.com/usebraid/braid/internal/server/derive/derivetest"
)

func day(n int) int { return n * 86400 }

// seed stores and derives three sessions of the user and returns the two device ids:
//
//	s1  /work/alpha  d1  2026-11-01  title "Alpha work" (ai-title), subagent a1, a Skill, an Agent call, a Bash error, a server_error;
//	s2  /work/beta   d1  2026-11-02  a usage_limit failure at +600 s resetting at +7800 s, then a successful reply at +2400 s;
//	s3  /work/alpha  d2  2026-11-03  title "Custom gamma" (custom-title.json).
func seed(t *testing.T, x *env) (d1, d2 string) {
	t.Helper()
	d1 = x.dev
	d2 = x.dt.Device(t, x.uid, "desktop")
	beta := func(l string) string { return dt.Cwd(l, "/work/beta") }
	x.dt.Put(t, x.uid, d1,
		dt.File{Rel: "-work-alpha/s1.jsonl", Lines: []string{
			dt.Prompt(dt.At(0), "Alpha prompt about zebra"),
			dt.Reply(dt.At(10), "m1", opus, dt.Usage(1000, 100, 0, 0, 0), dt.Text("looking into quokka"),
				dt.Tool("sk1", "Skill", map[string]any{"skill": "superpowers:writing-plans"}), dt.Tool("tuA", "Agent", map[string]any{"description": "explore"})),
			dt.Result(dt.At(20), "tuA", false, map[string]any{"agentId": "a1", "status": "completed", "totalDurationMs": 5000}),
			dt.Reply(dt.At(30), "m2", opus, dt.Usage(2000, 200, 0, 0, 0), dt.Tool("t1", "Bash", map[string]any{"command": "go test"})),
			dt.Result(dt.At(40), "t1", true, map[string]any{}),
			dt.APIError(dt.At(45), "error", "server_error"),
			dt.Line("ai-title", "", "aiTitle", "Alpha work"),
			dt.Turn(dt.At(50), 3000),
		}},
		dt.File{Rel: "-work-alpha/s1/subagents/agent-a1.meta.json", JSON: `{"agentType":"Explore","description":"explore","toolUseId":"tuA","spawnDepth":1,"finished":true}`},
		dt.File{Rel: "-work-alpha/s1/subagents/agent-a1.jsonl", Lines: []string{
			dt.Side(dt.SidePrompt(dt.At(11), "sub"), "a1"),
			dt.Side(dt.Reply(dt.At(15), "m3", sonnet, dt.Usage(500, 50, 0, 0, 0), dt.Tool("t2", "Read", map[string]any{"file_path": "/work/alpha/internal/handler.go"})), "a1"),
			dt.Side(dt.Result(dt.At(16), "t2", false, map[string]any{"durationMs": 30}), "a1"),
		}},
		dt.File{Rel: "-work-alpha/s2.jsonl", Lines: []string{
			beta(dt.Prompt(dt.At(day(1)), "Beta prompt")),
			beta(dt.Reply(dt.At(day(1)+60), "m4", opus, dt.Usage(100, 10, 0, 0, 0), dt.Text("beta answer"))),
			beta(dt.APIError(dt.At(day(1)+600), "error", "rate_limit", "apiError", "usage_limit_reached", "apiErrorStatus", 429,
				"quotaLimits", map[string]any{"resetsAt": dt.Unix(day(1) + 7800), "rateLimitType": "five_hour"})),
			beta(dt.Reply(dt.At(day(1)+2400), "m5", opus, dt.Usage(100, 10, 0, 0, 0), dt.Text("beta again"))),
			beta(dt.Turn(dt.At(day(1)+2500), 1000)),
		}})
	x.dt.Put(t, x.uid, d2,
		dt.File{Rel: "-work-alpha/s3.jsonl", Lines: []string{
			dt.Prompt(dt.At(day(2)), "Gamma prompt"),
			dt.Reply(dt.At(day(2)+60), "m6", opus, dt.Usage(10, 1, 0, 0, 0), dt.Text("gamma")),
		}},
		dt.File{Rel: "-work-alpha/s3/custom-title.json", JSON: `{"customTitle":"Custom gamma"}`})
	x.dt.Derive(t, x.uid, 0)
	return d1, d2
}
