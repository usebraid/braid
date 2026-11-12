// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package derive_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	dt "github.com/usebraid/braid/internal/server/derive/derivetest"
)

const opus, sonnet = "claude-opus-5-5", "claude-sonnet-5-5"

func setup(t *testing.T) (*dt.Env, string, string) {
	t.Helper()
	e := dt.New(t)
	u := e.User(t, "u1")
	return e, u, e.Device(t, u, "laptop")
}

func TestEntitiesFromAMainTranscript(t *testing.T) {
	e, u, d := setup(t)
	e.Put(t, u, d, dt.File{Rel: "-work-alpha/s1.jsonl", Lines: []string{
		dt.Prompt(dt.At(0), "Refactor the parser please"),
		dt.Reply(dt.At(5), "m1", opus, dt.Usage(1000, 10, 0, 0, 0), dt.Text("ok")),
		dt.Reply(dt.At(6), "m2", opus, dt.Usage(2000, 5, 100, 0, 0), dt.Tool("t1", "Bash", map[string]any{"command": "go test ./..."})),
		dt.Reply(dt.At(7), "m2", opus, dt.Usage(2000, 50, 100, 0, 0)), // same message id, streamed further
		dt.Result(dt.At(9), "t1", false, map[string]any{"durationMs": 1200}),
		dt.Line("ai-title", "", "aiTitle", "Parser refactor"),
	}})
	if n := e.Derive(t, u, 0); n != 6 {
		t.Fatalf("rows %d", n)
	}
	if got := e.Row(t, `SELECT title, first_prompt, project_path, line_count, primary_device_id::text, array_to_string(models, ',') FROM sessions WHERE session_id='s1'`); got != "Parser refactor|Refactor the parser please|/work/alpha|6|"+d+"|"+opus {
		t.Fatalf("session: %s", got)
	}
	if e.N(t, `SELECT count(*) FROM sessions WHERE started_at = $1::timestamptz AND ended_at = $2::timestamptz`, dt.At(0), dt.At(9)) != 1 {
		t.Fatal("session bounds")
	}
	if got := e.Row(t, `SELECT name FROM projects`); got != "alpha" {
		t.Fatalf("project %s", got)
	}
	if got := e.Row(t, `SELECT count(*), sum(output_tokens)::bigint FROM usage`); got != "2|60" {
		t.Fatalf("usage rows: %s", got)
	}
	if got := e.Row(t, `SELECT output_tokens, round(cost_usd*1e6)::bigint, price_unknown, agent_id FROM usage WHERE message_id='m2'`); got != "50|9020|false|main" {
		t.Fatalf("m2: %s", got)
	}
	if got := e.Row(t, `SELECT round(cost_usd*1e6)::bigint FROM usage WHERE message_id='m1'`); got != "4200" {
		t.Fatalf("m1 cost: %s", got)
	}
	if got := e.Row(t, `SELECT tool_name, agent_id, input_summary, duration_ms, is_error FROM tool_calls WHERE tool_use_id='t1'`); got != "Bash|main|go test ./...|1200|false" {
		t.Fatalf("tool call: %s", got)
	}
	if n := e.Derive(t, u, 0); n != 0 {
		t.Fatalf("second run processed %d rows", n)
	}
}

func TestUsageIsCountedOncePerMessageAcrossLinesAndFiles(t *testing.T) {
	e, u, d := setup(t)
	e.Put(t, u, d,
		dt.File{Rel: "-work-alpha/s1.jsonl", Lines: []string{
			dt.Prompt(dt.At(0), "p"),
			dt.Reply(dt.At(1), "m9", opus, dt.Usage(10, 10, 0, 0, 0)),
			dt.Reply(dt.At(2), "m9", opus, dt.Usage(10, 30, 0, 0, 0)),
			dt.Reply(dt.At(3), "m9", opus, dt.Usage(10, 20, 0, 0, 0)), // lower output arriving later never wins
		}},
		dt.File{Rel: "-work-alpha/s1/subagents/agent-a1.jsonl", Lines: []string{
			dt.Side(dt.Reply(dt.At(4), "m9", opus, dt.Usage(10, 30, 0, 0, 0)), "a1"), // same message in another file, equal output: lowest (file, offset) wins
		}})
	if n := e.Derive(t, u, 1); n != 5 { // one row per transaction
		t.Fatalf("rows %d", n)
	}
	if got := e.Row(t, `SELECT count(*), max(output_tokens), min(agent_id) FROM usage WHERE message_id='m9'`); got != "1|30|main" {
		t.Fatalf("usage: %s", got)
	}
}

func TestUnparsableLinesAreCountedAndSkipped(t *testing.T) {
	e, u, d := setup(t)
	e.Put(t, u, d, dt.File{Rel: "-work-alpha/s1.jsonl", Lines: []string{"not json", dt.Prompt(dt.At(0), "hi"), `{"type":"mode","mode":"auto"}`, `{"type":"brand-new","x":1}`}})
	if n := e.Derive(t, u, 0); n != 4 {
		t.Fatalf("rows %d", n)
	}
	if got := e.Row(t, `SELECT errors FROM derive_errors`); got != "1" {
		t.Fatalf("errors %s", got)
	}
	if got := e.Row(t, `SELECT line_count FROM sessions`); got != "4" {
		t.Fatalf("line_count %s", got)
	}
	if n := e.Derive(t, u, 0); n != 0 {
		t.Fatalf("cursor did not advance past the bad line: %d", n)
	}
}

func TestSessionDeviceIsTheOneWithMostLines(t *testing.T) {
	e, u, d1 := setup(t)
	d2 := e.Device(t, u, "desktop")
	e.Put(t, u, d1, dt.File{Rel: "-work-alpha/s1.jsonl", Lines: []string{dt.Prompt(dt.At(0), "p"), dt.Reply(dt.At(1), "m1", opus, dt.Usage(1, 1, 0, 0, 0))}})
	e.Put(t, u, d2, dt.File{Rel: "-work-alpha/s1.jsonl", Lines: []string{dt.Reply(dt.At(2), "m2", opus, dt.Usage(1, 1, 0, 0, 0))}}) // synced file, appended elsewhere
	e.Derive(t, u, 0)
	if got := e.Row(t, `SELECT primary_device_id::text FROM sessions`); got != d1 {
		t.Fatalf("primary %s", got)
	}
	if e.N(t, `SELECT count(*) FROM session_devices WHERE device_id=$1 AND line_count=2`, d1) != 1 ||
		e.N(t, `SELECT count(*) FROM session_devices WHERE device_id=$1 AND line_count=1`, d2) != 1 {
		t.Fatal("per-device line counts")
	}
}

func TestTitlesPreferCustomThenAIThenPrompt(t *testing.T) {
	e, u, d := setup(t)
	long := strings.Repeat("é", 100)
	e.Put(t, u, d, dt.File{Rel: "-work-alpha/s2.jsonl", Lines: []string{dt.Prompt(dt.At(0), "  spaced   out\n prompt  "+long)}})
	e.Derive(t, u, 0)
	if got := e.Row(t, `SELECT char_length(title), left(title, 20) FROM sessions WHERE session_id='s2'`); got != "80|spaced out prompt éé" {
		t.Fatalf("prompt title: %s", got)
	}
	e.Put(t, u, d, dt.File{Rel: "-work-alpha/s2.jsonl", Lines: []string{dt.Line("ai-title", "", "aiTitle", "AI title")}})
	e.Derive(t, u, 0)
	if got := e.Row(t, `SELECT title FROM sessions WHERE session_id='s2'`); got != "AI title" {
		t.Fatalf("ai title: %s", got)
	}
	e.Put(t, u, d, dt.File{Rel: "-work-alpha/s2/custom-title.json", JSON: `{"customTitle":"First"}`})
	e.Put(t, u, d, dt.File{Rel: "-work-alpha/s2/custom-title.json", JSON: `{"customTitle":"Second"}`}) // only the newest snapshot is used
	if n := e.Derive(t, u, 0); n != 1 {
		t.Fatalf("rows %d (older snapshot versions must be skipped)", n)
	}
	if got := e.Row(t, `SELECT title FROM sessions WHERE session_id='s2'`); got != "Second" {
		t.Fatalf("custom title: %s", got)
	}
}

func TestAgentRowsFromMetaAndSubagentFiles(t *testing.T) {
	e, u, d := setup(t)
	e.Put(t, u, d,
		dt.File{Rel: "-work-alpha/s1.jsonl", Lines: []string{dt.Prompt(dt.At(0), "p"), dt.Reply(dt.At(1), "m1", opus, dt.Usage(1, 1, 0, 0, 0))}},
		dt.File{Rel: "-work-alpha/s1/subagents/agent-a1.meta.json", JSON: `{"agentType":"Explore","description":"look around","toolUseId":"tu1","spawnDepth":1,"finished":true}`},
		dt.File{Rel: "-work-alpha/s1/subagents/agent-a1.jsonl", Lines: []string{
			dt.Side(dt.SidePrompt(dt.At(20), "sub task"), "a1"),
			dt.Side(dt.Reply(dt.At(40), "m2", opus, dt.Usage(1, 1, 0, 0, 0)), "a1")}})
	e.Derive(t, u, 0)
	if got := e.Row(t, `SELECT agent_type, description, spawn_tool_use_id, spawn_depth, finished FROM agents WHERE agent_id='a1'`); got != "Explore|look around|tu1|1|true" {
		t.Fatalf("a1: %s", got)
	}
	if e.N(t, `SELECT count(*) FROM agents WHERE agent_id='a1' AND started_at=$1::timestamptz AND ended_at=$2::timestamptz`, dt.At(20), dt.At(40)) != 1 {
		t.Fatal("a1 bounds")
	}
	if got := e.Row(t, `SELECT agent_type FROM agents WHERE agent_id='main'`); got != "main" {
		t.Fatalf("main: %s", got)
	}
	if got := e.Row(t, `SELECT count(*) FROM usage WHERE agent_id='a1'`); got != "1" {
		t.Fatalf("subagent usage: %s", got)
	}
}

func TestDerivedRowsStayWithTheirUser(t *testing.T) {
	e, u1, d1 := setup(t)
	u2 := e.User(t, "u2")
	d2 := e.Device(t, u2, "other")
	e.Put(t, u1, d1, dt.File{Rel: "-work-alpha/s1.jsonl", Lines: []string{dt.Prompt(dt.At(0), "mine")}})
	e.Put(t, u2, d2, dt.File{Rel: "-work-alpha/s1.jsonl", Lines: []string{dt.Prompt(dt.At(0), "theirs")}}) // same rel path and session id, other user
	e.Derive(t, u1, 0)
	e.Derive(t, u2, 0)
	if got := e.Row(t, `SELECT string_agg(title, ',' ORDER BY title) FROM sessions`); got != "mine,theirs" {
		t.Fatalf("%s", got)
	}
	err := e.DB.WithUser(context.Background(), u2, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE title = 'mine'`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Errorf("user 2 sees user 1's session")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	e.Reset(t, u1)
	if e.N(t, `SELECT count(*) FROM sessions`) != 1 || e.N(t, `SELECT count(*) FROM derive_cursor WHERE user_id = $1`, u1) != 0 {
		t.Fatal("Reset touched the wrong rows")
	}
}
