// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package derivetest

const opus, sonnet = "claude-opus-5-5", "claude-sonnet-5-5"

// Scenario returns the files of one session "g1" (main + 3 subagents incl. a nested one) in a fixed order.
func Scenario() []File {
	fast := Usage(10, 10, 0, 0, 0)
	fast["speed"] = "fast"
	searches := Usage(0, 0, 0, 0, 0)
	searches["server_tool_use"] = map[string]any{"web_search_requests": 2, "web_fetch_requests": 0}
	return []File{
		{Rel: "-work-alpha/g1.jsonl", Lines: []string{
			Line("mode", "", "mode", "auto"),
			Line("pr-link", At(1), "prNumber", 7, "prUrl", "https://example.invalid/pr/7"),
			Line("attachment", At(2), "attachment", map[string]any{"type": "hook"}),
			Line("api-request", At(3), "id", "r1"),
			Line("api-request-blob", "", "hash", "h"),
			Prompt(At(10), "Golden prompt"),
			Reply(At(20), "g-m1", opus, Usage(1000, 100, 500, 200, 300), Text("a"),
				Tool("sk1", "Skill", map[string]any{"skill": "superpowers:brainstorming"}), Tool("tuA", "Agent", map[string]any{"description": "explore"})),
			Reply(At(21), "g-m1", opus, Usage(1000, 150, 500, 200, 300)), // same message, streamed further
			Result(At(30), "tuA", false, map[string]any{"agentId": "a1", "status": "completed", "totalDurationMs": 9000}),
			Reply(At(40), "g-m2", sonnet, Usage(2000, 200, 0, 0, 0),
				Tool("tuB", "Agent", map[string]any{"description": "async", "run_in_background": true}), Tool("t1", "Bash", map[string]any{"command": "go test"})),
			Result(At(41), "tuB", false, map[string]any{"agentId": "a2", "status": "async_launched"}),
			Result(At(45), "t1", true, map[string]any{"durationMs": 50}),
			Reply(At(60), "g-m3", "claude-fable-5-2", Usage(100, 10, 0, 0, 0)), // model without a price
			Reply(At(70), "g-m4", opus, fast),
			Reply(At(80), "g-m5", opus, searches),
			Notify(At(90), "tuB", 12000),
			Turn(At(100), 2000),
			Turn(At(200), 3000),
			APIError(At(300), "error", "rate_limit", "apiError", "usage_limit_reached", "apiErrorStatus", 429,
				"quotaLimits", map[string]any{"resetsAt": Unix(300 + 3600), "rateLimitType": "five_hour"}),
			Reply(At(900), "g-m6", opus, Usage(1, 1, 0, 0, 0)),
			APIError(At(910), "error", "server_error", "apiError", "computer_slept"),
			Line("system", At(920), "subtype", "model_refusal_fallback", "apiRefusalCategory", "cyber"),
			APIError(At(930), "error", "rate_limit"),
			APIError(At(935), "error", "server_error"),
			APIError(At(940), "error", "invalid_request"),
			Line("ai-title", "", "aiTitle", "Golden AI"),
			Reply(At(950), "g-m7", opus, Usage(1, 1, 0, 0, 0), Tool("sk2", "Skill", map[string]any{"skill": "superpowers:writing-plans"})),
			Line("brand-new-type", At(960), "x", 1),
			"not json",
		}},
		{Rel: "-work-alpha/g1/custom-title.json", JSON: `{"customTitle":"Golden custom"}`},
		{Rel: "-work-alpha/g1/subagents/agent-a1.meta.json", JSON: `{"agentType":"Explore","description":"d1","toolUseId":"tuA","spawnDepth":1,"finished":true}`},
		{Rel: "-work-alpha/g1/subagents/agent-a1.jsonl", Lines: []string{
			Side(SidePrompt(At(22), "sub task"), "a1"),
			Side(Reply(At(25), "g-s1", sonnet, Usage(500, 50, 0, 0, 0),
				Tool("ssk", "Skill", map[string]any{"skill": "x:debug"}), Tool("tuC", "Agent", map[string]any{"description": "nested"})), "a1"),
		}},
		{Rel: "-work-alpha/g1/subagents/agent-a2.meta.json", JSON: `{"agentType":"general-purpose","description":"d2","toolUseId":"tuB","spawnDepth":1,"finished":true}`},
		{Rel: "-work-alpha/g1/subagents/agent-a2.jsonl", Lines: []string{
			Side(SidePrompt(At(42), "async task"), "a2"),
			Side(Reply(At(50), "g-s2", sonnet, Usage(300, 30, 0, 0, 0)), "a2"),
		}},
		{Rel: "-work-alpha/g1/subagents/agent-a3.meta.json", JSON: `{"agentType":"Explore","description":"d3","toolUseId":"tuC","spawnDepth":2}`},
		{Rel: "-work-alpha/g1/subagents/agent-a3.jsonl", Lines: []string{
			Side(SidePrompt(At(27), "nested task"), "a3"),
			Side(Reply(At(35), "g-s3", sonnet, Usage(100, 10, 0, 0, 0)), "a3"),
		}},
	}
}
