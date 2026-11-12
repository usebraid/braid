// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package derive_test

import (
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	dt "github.com/usebraid/braid/internal/server/derive/derivetest"
)

var update = flag.Bool("update", false, "rewrite testdata golden files")

// scenario returns the files of one session "g1" (main + 3 subagents incl. a nested one) in a fixed order.
func scenario() []dt.File {
	fast := dt.Usage(10, 10, 0, 0, 0)
	fast["speed"] = "fast"
	searches := dt.Usage(0, 0, 0, 0, 0)
	searches["server_tool_use"] = map[string]any{"web_search_requests": 2, "web_fetch_requests": 0}
	return []dt.File{
		{Rel: "-work-alpha/g1.jsonl", Lines: []string{
			dt.Line("mode", "", "mode", "auto"),
			dt.Line("pr-link", dt.At(1), "prNumber", 7, "prUrl", "https://example.invalid/pr/7"),
			dt.Line("attachment", dt.At(2), "attachment", map[string]any{"type": "hook"}),
			dt.Line("api-request", dt.At(3), "id", "r1"),
			dt.Line("api-request-blob", "", "hash", "h"),
			dt.Prompt(dt.At(10), "Golden prompt"),
			dt.Reply(dt.At(20), "g-m1", opus, dt.Usage(1000, 100, 500, 200, 300), dt.Text("a"),
				dt.Tool("sk1", "Skill", map[string]any{"skill": "superpowers:brainstorming"}), dt.Tool("tuA", "Agent", map[string]any{"description": "explore"})),
			dt.Reply(dt.At(21), "g-m1", opus, dt.Usage(1000, 150, 500, 200, 300)), // same message, streamed further
			dt.Result(dt.At(30), "tuA", false, map[string]any{"agentId": "a1", "status": "completed", "totalDurationMs": 9000}),
			dt.Reply(dt.At(40), "g-m2", sonnet, dt.Usage(2000, 200, 0, 0, 0),
				dt.Tool("tuB", "Agent", map[string]any{"description": "async"}), dt.Tool("t1", "Bash", map[string]any{"command": "go test"})),
			dt.Result(dt.At(41), "tuB", false, map[string]any{"agentId": "a2", "status": "async_launched"}),
			dt.Result(dt.At(45), "t1", true, map[string]any{"durationMs": 50}),
			dt.Reply(dt.At(60), "g-m3", "claude-fable-5-2", dt.Usage(100, 10, 0, 0, 0)), // model without a price
			dt.Reply(dt.At(70), "g-m4", opus, fast),
			dt.Reply(dt.At(80), "g-m5", opus, searches),
			dt.Notify(dt.At(90), "tuB", 12000),
			dt.Turn(dt.At(100), 2000),
			dt.Turn(dt.At(200), 3000),
			dt.APIError(dt.At(300), "error", "rate_limit", "apiError", "usage_limit_reached", "apiErrorStatus", 429,
				"quotaLimits", map[string]any{"resetsAt": dt.Unix(300 + 3600), "rateLimitType": "five_hour"}),
			dt.Reply(dt.At(900), "g-m6", opus, dt.Usage(1, 1, 0, 0, 0)),
			dt.APIError(dt.At(910), "error", "server_error", "apiError", "computer_slept"),
			dt.Line("system", dt.At(920), "subtype", "model_refusal_fallback", "apiRefusalCategory", "cyber"),
			dt.APIError(dt.At(930), "error", "rate_limit"),
			dt.APIError(dt.At(935), "error", "server_error"),
			dt.APIError(dt.At(940), "error", "invalid_request"),
			dt.Line("ai-title", "", "aiTitle", "Golden AI"),
			dt.Reply(dt.At(950), "g-m7", opus, dt.Usage(1, 1, 0, 0, 0), dt.Tool("sk2", "Skill", map[string]any{"skill": "superpowers:writing-plans"})),
			dt.Line("brand-new-type", dt.At(960), "x", 1),
			"not json",
		}},
		{Rel: "-work-alpha/g1/custom-title.json", JSON: `{"customTitle":"Golden custom"}`},
		{Rel: "-work-alpha/g1/subagents/agent-a1.meta.json", JSON: `{"agentType":"Explore","description":"d1","toolUseId":"tuA","spawnDepth":1,"finished":true}`},
		{Rel: "-work-alpha/g1/subagents/agent-a1.jsonl", Lines: []string{
			dt.Side(dt.SidePrompt(dt.At(22), "sub task"), "a1"),
			dt.Side(dt.Reply(dt.At(25), "g-s1", sonnet, dt.Usage(500, 50, 0, 0, 0),
				dt.Tool("ssk", "Skill", map[string]any{"skill": "x:debug"}), dt.Tool("tuC", "Agent", map[string]any{"description": "nested"})), "a1"),
		}},
		{Rel: "-work-alpha/g1/subagents/agent-a2.meta.json", JSON: `{"agentType":"general-purpose","description":"d2","toolUseId":"tuB","spawnDepth":1,"finished":true}`},
		{Rel: "-work-alpha/g1/subagents/agent-a2.jsonl", Lines: []string{
			dt.Side(dt.SidePrompt(dt.At(42), "async task"), "a2"),
			dt.Side(dt.Reply(dt.At(50), "g-s2", sonnet, dt.Usage(300, 30, 0, 0, 0)), "a2"),
		}},
		{Rel: "-work-alpha/g1/subagents/agent-a3.meta.json", JSON: `{"agentType":"Explore","description":"d3","toolUseId":"tuC","spawnDepth":2}`},
		{Rel: "-work-alpha/g1/subagents/agent-a3.jsonl", Lines: []string{
			dt.Side(dt.SidePrompt(dt.At(27), "nested task"), "a3"),
			dt.Side(dt.Reply(dt.At(35), "g-s3", sonnet, dt.Usage(100, 10, 0, 0, 0)), "a3"),
		}},
	}
}

func TestGoldenFullScenario(t *testing.T) {
	e, u, d := setup(t)
	e.Put(t, u, d, scenario()...)
	e.Derive(t, u, 0)

	checks := map[string]string{ // independent of the golden file: figures worked out by hand from the price table (fast mode and the unknown fable-5-2 are estimated, not zero)
		`SELECT count(*), count(*) FILTER (WHERE price_unknown), count(*) FILTER (WHERE price_estimated), round(sum(cost_usd)*1e6)::bigint FROM usage`: "10|0|2|40988",
		`SELECT sum(input_tokens+output_tokens+cache_read_tokens+cache_write_5m_tokens+cache_write_1h_tokens)::bigint FROM usage`:                      "5474",
		`SELECT round(cost_usd*1e6)::bigint FROM usage WHERE message_id='g-m1'`:                                                                        "10500", // 1000*4+200*5+300*8+500*0.2+150*20 (final streamed output wins)
		`SELECT round(cost_usd*1e6)::bigint FROM usage WHERE message_id='g-m5'`:                                                                        "20000", // two web searches
		`SELECT title || '|' || line_count || '|' || project_path FROM sessions`:                                                                       "Golden custom|35|/work/alpha",
		`SELECT string_agg(agent_id || '=' || duration_ms || '/' || duration_estimated, ',' ORDER BY agent_id) FROM agents`:                            "a1=9000/false,a2=12000/false,a3=8000/true,main=5000/false",
		`SELECT string_agg(agent_id || '>' || coalesce(parent_agent_id, '-'), ',' ORDER BY agent_id) FROM agents`:                                      "a1>main,a2>main,a3>a1,main>-",
		`SELECT string_agg(category || '=' || n, ',' ORDER BY category) FROM (SELECT category, count(*) n FROM failures GROUP BY 1) x`:                 "network=1,other_api_error=1,rate_limit=1,refusal=1,server_error=1,tool_error=1,usage_limit=1",
		`SELECT string_agg(agent_id || ':' || seq || ':' || skill, ',' ORDER BY agent_id, seq) FROM skill_phases`:                                      "a1:1:x:debug,main:1:superpowers:brainstorming,main:2:superpowers:writing-plans",
		`SELECT errors FROM derive_errors`: "1",
		`SELECT count(*) FROM search_docs`: "5",
	}
	for q, want := range checks {
		if got := e.Row(t, q); got != want {
			t.Errorf("%s\n got  %s\n want %s", q, got, want)
		}
	}

	got := e.Dump(t, u, map[string]string{u: "USER", d: "DEVICE"})
	golden := filepath.Join("testdata", "golden_full.txt")
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden) //nolint:gosec // fixed path under testdata
	if err != nil {
		t.Fatalf("%v (run: go test ./internal/server/derive -run Golden -update)", err)
	}
	if string(want) != got {
		t.Fatalf("derived tables differ from %s (rerun with -update only after checking the diff):\n%s", golden, got)
	}
}

func TestDerivationIsIdempotentChunkIndependentAndEqualsReindex(t *testing.T) {
	e := dt.New(t)
	files := scenario()
	repl := func(u, d string) map[string]string { return map[string]string{u: "USER", d: "DEVICE"} }
	newUser := func(sub string) (string, string) {
		u := e.User(t, sub)
		return u, e.Device(t, u, "laptop")
	}
	u0, d0 := newUser("base")
	e.Put(t, u0, d0, files...)
	e.Derive(t, u0, 0)
	base := e.Dump(t, u0, repl(u0, d0))
	if e.Derive(t, u0, 0) != 0 || e.Dump(t, u0, repl(u0, d0)) != base {
		t.Fatal("a second run changed something")
	}
	e.Reset(t, u0)
	e.Derive(t, u0, 0)
	if e.Dump(t, u0, repl(u0, d0)) != base {
		t.Fatal("reindex differs from the incremental result")
	}
	rnd := rand.New(rand.NewPCG(7, 11)) //nolint:gosec // deterministic shuffle of test files
	for i, batch := range []int{1, 2, 3, 7, 1000} {
		u, d := newUser(fmt.Sprintf("chunk%d", i))
		order := rnd.Perm(len(files))
		for _, j := range order {
			e.Put(t, u, d, files[j])
		}
		e.Derive(t, u, batch)
		if got := e.Dump(t, u, repl(u, d)); got != base {
			t.Fatalf("batch %d, file order %v differs from the baseline", batch, order)
		}
	}
	u, d := newUser("incremental") // raw data arriving in two parts, derived in between
	main := files[0]
	half := len(main.Lines) / 2
	e.Put(t, u, d, dt.File{Rel: main.Rel, Lines: main.Lines[:half]})
	e.Derive(t, u, 0)
	e.Put(t, u, d, dt.File{Rel: main.Rel, Lines: main.Lines[half:]})
	e.Put(t, u, d, files[1:]...)
	e.Derive(t, u, 0)
	if got := e.Dump(t, u, repl(u, d)); got != base {
		t.Fatal("incremental result differs from the full run")
	}
}
