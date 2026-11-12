// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package derive_test

import (
	"testing"

	dt "github.com/usebraid/braid/internal/server/derive/derivetest"
)

func TestFailureRowsPerCategory(t *testing.T) {
	e, u, d := setup(t)
	resets := dt.Unix(7500)
	e.Put(t, u, d,
		dt.File{Rel: "-work-alpha/s1.jsonl", Lines: []string{
			dt.Prompt(dt.At(0), "go"),
			dt.APIError(dt.At(100), "error", "rate_limit", "apiError", "usage_limit_reached", "apiErrorStatus", 429,
				"quotaLimits", map[string]any{"status": "rejected", "resetsAt": resets, "rateLimitType": "five_hour"}),
			dt.APIError(dt.At(110), "error", "server_error", "apiError", "computer_slept"),
			dt.APIError(dt.At(120), "error", "unknown", "apiErrorStatus", 429),
			dt.APIError(dt.At(130), "error", "server_error"),
			dt.APIError(dt.At(140), "error", "invalid_request"),
			dt.Line("system", dt.At(150), "subtype", "model_refusal_no_fallback", "apiRefusalCategory", "cyber"),
			dt.Line("system", dt.At(155), "subtype", "api_error", "retryAttempt", 1), // retry noise, not a failure
			dt.Result(dt.At(160), "t9", true, map[string]any{}),
		}},
		dt.File{Rel: "-work-alpha/s1/subagents/agent-a1.jsonl", Lines: []string{
			dt.Side(dt.APIError(dt.At(170), "error", "overloaded_error", "apiErrorStatus", 529), "a1"),
		}})
	e.Derive(t, u, 3)
	want := "network=1,other_api_error=1,rate_limit=1,refusal=1,server_error=2,tool_error=1,usage_limit=1"
	if got := e.Row(t, `SELECT string_agg(category || '=' || n, ',' ORDER BY category) FROM (SELECT category, count(*) n FROM failures GROUP BY 1) x`); got != want {
		t.Fatalf("%s", got)
	}
	if got := e.Row(t, `SELECT limit_type, detail, session_id, agent_id, resets_at = to_timestamp($1::bigint) FROM failures WHERE category='usage_limit'`, resets); got != "five_hour|rate_limit/usage_limit_reached/429|s1|main|true" {
		t.Fatalf("usage limit: %s", got)
	}
	if got := e.Row(t, `SELECT detail FROM failures WHERE category='tool_error'`); got != "error" {
		t.Fatalf("tool error detail: %s", got)
	}
	if got := e.Row(t, `SELECT agent_id FROM failures WHERE detail LIKE 'overloaded_error%'`); got != "a1" {
		t.Fatalf("subagent failure: %s", got)
	}
	if e.N(t, `SELECT count(*) FROM usage`) != 0 {
		t.Fatal("error lines produced usage")
	}
	n := e.N(t, `SELECT count(*) FROM failures`)
	e.Reset(t, u)
	e.Derive(t, u, 0)
	if e.N(t, `SELECT count(*) FROM failures`) != n {
		t.Fatal("reindex changed the failures")
	}
}
