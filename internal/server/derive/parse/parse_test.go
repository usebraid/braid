// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package parse

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func kinds(evs []Event) []Kind {
	var k []Kind
	for _, e := range evs {
		k = append(k, e.Kind)
	}
	return k
}

func parse(t *testing.T, s string) []Event {
	t.Helper()
	evs, err := ParseLine([]byte(s))
	if err != nil {
		t.Fatalf("%v for %s", err, s)
	}
	return evs
}

func find(evs []Event, k Kind) *Event {
	for i := range evs {
		if evs[i].Kind == k {
			return &evs[i]
		}
	}
	return nil
}

const asst = `{"type":"assistant","timestamp":"2026-11-01T10:00:05.250Z","cwd":"/w/p","gitBranch":"main","isSidechain":false,
 "message":{"id":"msg_1","model":"claude-opus-5-5","role":"assistant","content":[
   {"type":"thinking","thinking":""},{"type":"text","text":"hello world"},
   {"type":"tool_use","id":"tu1","name":"Skill","input":{"skill":"superpowers:writing-plans"}},
   {"type":"tool_use","id":"tu2","name":"Edit","input":{"file_path":"/w/p/a.go","old_string":"SECRET","new_string":"SECRET2"}}],
  "usage":{"input_tokens":3,"output_tokens":50,"cache_read_input_tokens":7,"cache_creation_input_tokens":30,
   "cache_creation":{"ephemeral_5m_input_tokens":10,"ephemeral_1h_input_tokens":20},
   "output_tokens_details":{"thinking_tokens":9},"server_tool_use":{"web_search_requests":2,"web_fetch_requests":1},"speed":"standard"}}}`

func TestAssistantLine(t *testing.T) {
	evs := parse(t, asst)
	if got := kinds(evs); !slices.Equal(got, []Kind{KindTouch, KindAssistant, KindToolUse, KindToolUse}) {
		t.Fatalf("kinds %v", got)
	}
	tc := find(evs, KindTouch)
	if tc.CWD != "/w/p" || tc.Branch != "main" || tc.Sidechain || !tc.TS.Equal(time.Date(2026, 11, 1, 10, 0, 5, 250_000_000, time.UTC)) {
		t.Fatalf("%+v", tc)
	}
	a := find(evs, KindAssistant)
	want := Usage{ID: "msg_1", Model: "claude-opus-5-5", Speed: "standard", Input: 3, Output: 50, CacheRead: 7, CacheWrite5m: 10, CacheWrite1h: 20, Thinking: 9, WebSearch: 2, WebFetch: 1}
	if a.Text != "hello world" || a.Usage == nil || *a.Usage != want {
		t.Fatalf("%+v %+v", a, a.Usage)
	}
	var skill, edit *Event
	for i := range evs {
		if evs[i].Kind == KindToolUse && evs[i].Tool == "Skill" {
			skill = &evs[i]
		}
		if evs[i].Kind == KindToolUse && evs[i].Tool == "Edit" {
			edit = &evs[i]
		}
	}
	if skill.Skill != "superpowers:writing-plans" || skill.ToolUseID != "tu1" {
		t.Fatalf("%+v", skill)
	}
	if edit.Summary != "/w/p/a.go" || edit.Idents != "Edit /w/p/a.go" || strings.Contains(edit.Summary+edit.Idents+edit.Text, "SECRET") {
		t.Fatalf("%+v", edit)
	}
}

func TestUsageWithoutCacheSplitCountsAs5m(t *testing.T) {
	evs := parse(t, `{"type":"assistant","timestamp":"2026-11-01T10:00:00Z","message":{"id":"m","model":"claude-opus-5-5","content":[],
	  "usage":{"input_tokens":1,"output_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":4}}}`)
	if u := find(evs, KindAssistant).Usage; u == nil || u.CacheWrite5m != 4 || u.CacheWrite1h != 0 {
		t.Fatalf("%+v", u)
	}
}

func TestSyntheticAndErrorAssistantHaveNoUsage(t *testing.T) {
	evs := parse(t, `{"type":"assistant","timestamp":"2026-11-01T10:00:00Z","isApiErrorMessage":true,"error":"rate_limit","apiError":"usage_limit_reached","apiErrorStatus":429,
	  "quotaLimits":{"status":"rejected","resetsAt":1793590800,"rateLimitType":"five_hour"},
	  "message":{"id":"x","model":"<synthetic>","content":[{"type":"text","text":"limit reached"}],"usage":{"input_tokens":0,"output_tokens":0}}}`)
	if got := kinds(evs); !slices.Equal(got, []Kind{KindTouch, KindFailure}) {
		t.Fatalf("kinds %v", got)
	}
	f := find(evs, KindFailure).Failure
	if f.Category != "usage_limit" || f.LimitType != "five_hour" || f.ResetsAt.Unix() != 1793590800 || f.Detail != "rate_limit/usage_limit_reached/429" {
		t.Fatalf("%+v", f)
	}
	syn := parse(t, `{"type":"assistant","timestamp":"2026-11-01T10:00:00Z","message":{"id":"x","model":"<synthetic>","content":[],"usage":{"input_tokens":0}}}`)
	if find(syn, KindAssistant).Usage != nil {
		t.Fatal("synthetic usage kept")
	}
}

func TestFailureCategories(t *testing.T) {
	cases := []struct{ name, fields, cat, detail string }{
		{"slept", `"error":"server_error","apiError":"computer_slept"`, "network", "server_error/computer_slept"},
		{"conn", `"apiError":"Connection error"`, "network", "Connection error"},
		{"429", `"error":"unknown","apiErrorStatus":429`, "rate_limit", "unknown/429"},
		{"rate", `"error":"rate_limit"`, "rate_limit", "rate_limit"},
		{"server", `"error":"server_error"`, "server_error", "server_error"},
		{"overloaded", `"error":"overloaded_error","apiErrorStatus":529`, "server_error", "overloaded_error/529"},
		{"5xx", `"error":"unknown","apiErrorStatus":503`, "server_error", "unknown/503"},
		{"other", `"error":"invalid_request"`, "other_api_error", "invalid_request"},
		{"bare", `"x":1`, "other_api_error", ""},
	}
	for _, c := range cases {
		evs := parse(t, `{"type":"assistant","timestamp":"2026-11-01T10:00:00Z","isApiErrorMessage":true,`+c.fields+`,"message":{"id":"x","model":"<synthetic>","content":[]}}`)
		f := find(evs, KindFailure)
		if f == nil || f.Failure.Category != c.cat || f.Failure.Detail != c.detail {
			t.Errorf("%s: %+v", c.name, f)
		}
	}
}

func TestUserLines(t *testing.T) {
	prompt := parse(t, `{"type":"user","timestamp":"2026-11-01T10:00:00Z","isSidechain":false,"origin":{"kind":"human"},"message":{"role":"user","content":"fix it"}}`)
	if p := find(prompt, KindPrompt); p == nil || p.Text != "fix it" {
		t.Fatalf("%+v", prompt)
	}
	legacy := parse(t, `{"type":"user","timestamp":"2026-11-01T10:00:00Z","message":{"role":"user","content":[{"type":"text","text":"old"}]}}`)
	if find(legacy, KindPrompt) == nil {
		t.Fatal("prompt without origin must count")
	}
	for name, s := range map[string]string{
		"sidechain": `{"type":"user","timestamp":"2026-11-01T10:00:00Z","isSidechain":true,"message":{"role":"user","content":"sub prompt"}}`,
		"system":    `{"type":"user","timestamp":"2026-11-01T10:00:00Z","origin":{"kind":"task-notification"},"message":{"role":"user","content":"x"}}`,
		"meta":      `{"type":"user","timestamp":"2026-11-01T10:00:00Z","isMeta":true,"message":{"role":"user","content":"x"}}`,
	} {
		if find(parse(t, s), KindPrompt) != nil {
			t.Errorf("%s counted as prompt", name)
		}
	}
	res := parse(t, `{"type":"user","timestamp":"2026-11-01T10:00:09Z","toolDenialKind":"user_rejected",
	  "message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu1","is_error":true,"content":"boom"}]},"toolUseResult":{"durationMs":1200}}`)
	r, f := find(res, KindToolResult), find(res, KindFailure)
	if r.ToolUseID != "tu1" || !r.IsError || *r.DurationMs != 1200 || r.AgentTotalMs != nil || f.Failure.Category != "tool_error" || f.Failure.Detail != "user_rejected" {
		t.Fatalf("%+v %+v", r, f)
	}
	agent := parse(t, `{"type":"user","timestamp":"2026-11-01T10:00:09Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tuA","content":"ok"}]},
	  "toolUseResult":{"status":"completed","agentId":"a1","totalDurationMs":44287}}`)
	if r := find(agent, KindToolResult); r.AgentTotalMs == nil || *r.AgentTotalMs != 44287 || r.DurationMs != nil || find(agent, KindFailure) != nil {
		t.Fatalf("%+v", r)
	}
	str := parse(t, `{"type":"user","timestamp":"2026-11-01T10:00:09Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu3","content":"x"}]},"toolUseResult":"plain string"}`)
	if r := find(str, KindToolResult); r == nil || r.DurationMs != nil {
		t.Fatalf("%+v", r)
	}
}

func TestSystemTitleAndNotificationLines(t *testing.T) {
	if e := find(parse(t, `{"type":"system","subtype":"turn_duration","timestamp":"2026-11-01T10:00:00Z","durationMs":2500,"messageCount":4}`), KindTurn); e == nil || *e.DurationMs != 2500 {
		t.Fatalf("%+v", e)
	}
	ref := find(parse(t, `{"type":"system","subtype":"model_refusal_no_fallback","timestamp":"2026-11-01T10:00:00Z","apiRefusalCategory":"cyber"}`), KindFailure)
	if ref.Failure.Category != "refusal" || ref.Failure.Detail != "model_refusal_no_fallback/cyber" {
		t.Fatalf("%+v", ref)
	}
	if got := kinds(parse(t, `{"type":"system","subtype":"api_error","timestamp":"2026-11-01T10:00:00Z","retryAttempt":1}`)); !slices.Equal(got, []Kind{KindTouch}) {
		t.Fatalf("api_error retry is not a failure: %v", got)
	}
	if e := find(parse(t, `{"type":"ai-title","aiTitle":"Parser work"}`), KindTitle); e == nil || !e.AI || e.Text != "Parser work" || !e.TS.IsZero() {
		t.Fatalf("%+v", e)
	}
	if e := find(parse(t, `{"type":"custom-title","customTitle":"Mine"}`), KindTitle); e == nil || e.AI || e.Text != "Mine" {
		t.Fatalf("%+v", e)
	}
	n := find(parse(t, `{"type":"queue-operation","operation":"enqueue","timestamp":"2026-11-01T10:00:00Z","content":"<task-notification>\n<task-id>x</task-id>\n<tool-use-id>tuB</tool-use-id>\n<status>completed</status>\n<usage><duration_ms>4200</duration_ms></usage>\n</task-notification>"}`), KindTaskNote)
	if n == nil || n.ToolUseID != "tuB" || *n.AgentTotalMs != 4200 {
		t.Fatalf("%+v", n)
	}
	if find(parse(t, `{"type":"queue-operation","operation":"remove","timestamp":"2026-11-01T10:00:00Z","content":"<task-notification><tool-use-id>t</tool-use-id><duration_ms>1</duration_ms>"}`), KindTaskNote) != nil {
		t.Fatal("remove operation parsed")
	}
}

func TestLenient(t *testing.T) {
	for _, s := range []string{
		`{"type":"api-request-blob","hash":"h","message":{"x":1}}`, `{"type":"pr-link","prNumber":3}`, `{"type":"mode","mode":"auto"}`,
		`{"type":"brand-new-type","timestamp":"2026-11-01T10:00:00Z","whatever":[1,2,3]}`, `{}`, `{"type":7}`,
	} {
		if _, err := ParseLine([]byte(s)); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
	for _, s := range []string{`not json`, ``, `{"type":`, "\xff\xfe"} {
		if _, err := ParseLine([]byte(s)); !errors.Is(err, ErrBadLine) {
			t.Errorf("%q: %v", s, err)
		}
	}
	evs := parse(t, `{"type":"user","timestamp":"2026-11-01T10:00:00Z","message":{"role":"user","content":"bad \udc00 utf8 ☃"}}`)
	if p := find(evs, KindPrompt); p == nil || !utf8.ValidString(p.Text) {
		t.Fatalf("%+v", p)
	}
	long := parse(t, `{"type":"user","timestamp":"2026-11-01T10:00:00Z","message":{"role":"user","content":"`+strings.Repeat("é", MaxText)+`"}}`)
	if p := find(long, KindPrompt); len(p.Text) > MaxText || !utf8.ValidString(p.Text) {
		t.Fatalf("text not capped: %d", len(p.Text))
	}
}

func TestClassifyPath(t *testing.T) {
	cases := map[string]FileInfo{
		"-w-p/0b1c.jsonl":                          {FileMain, "0b1c", "main"},
		"-w-p/0b1c/subagents/agent-a1.jsonl":       {FileSubagent, "0b1c", "a1"},
		"-w-p/0b1c/subagents/agent-a1.meta.json":   {FileAgentMeta, "0b1c", "a1"},
		"-w-p/0b1c/custom-title.json":              {FileCustomTitle, "0b1c", "main"},
		"-w-p/0b1c/subagents/agent-a1.prefix.json": {},
		"-w-p/0b1c.desktop-released.json":          {},
		"-w-p/notes.txt":                           {},
		"x.jsonl":                                  {},
		"-w-p/0b1c/subagents/agent-.jsonl":         {},
	}
	for rel, want := range cases {
		if got := ClassifyPath(rel); got != want {
			t.Errorf("%s: %+v want %+v", rel, got, want)
		}
	}
}

func TestSnapshots(t *testing.T) {
	m, err := ParseSnapshot(FileInfo{Kind: FileAgentMeta, SessionID: "s", AgentID: "a2"},
		[]byte(`{"agentType":"web-fetch","description":"d","toolUseId":"tuX","spawnDepth":2,"parentAgentId":"a1","finished":true,"model":"sonnet"}`))
	if err != nil || len(m) != 1 || m[0].Meta == nil {
		t.Fatalf("%v %+v", err, m)
	}
	got := *m[0].Meta
	if got.AgentType != "web-fetch" || got.ToolUseID != "tuX" || got.SpawnDepth != 2 || got.ParentAgentID != "a1" || got.Finished == nil || !*got.Finished {
		t.Fatalf("%+v", got)
	}
	c, err := ParseSnapshot(FileInfo{Kind: FileCustomTitle}, []byte(`{"customTitle":"Mine"}`))
	if err != nil || len(c) != 1 || c[0].Kind != KindCustomTitle || c[0].Text != "Mine" {
		t.Fatalf("%v %+v", err, c)
	}
	if _, err := ParseSnapshot(FileInfo{Kind: FileAgentMeta}, []byte(`nope`)); !errors.Is(err, ErrBadLine) {
		t.Fatalf("%v", err)
	}
}

func TestSummarize(t *testing.T) {
	long := strings.Repeat("x", 500)
	if s, _ := Summarize("Bash", map[string]any{"command": long}); utf8.RuneCountInString(s) != 200 {
		t.Fatalf("len %d", utf8.RuneCountInString(s))
	}
	if s, id := Summarize("Write", map[string]any{"file_path": "/w/a.go", "content": "SECRET"}); s != "/w/a.go" || id != "Write /w/a.go" {
		t.Fatalf("%q %q", s, id)
	}
	if s, _ := Summarize("mcp__x__create", map[string]any{"content": "SECRET", "query": "q1"}); s != "q1" {
		t.Fatalf("%q", s)
	}
	if s, id := Summarize("Weird", map[string]any{"content": "SECRET"}); s != "" || id != "Weird" {
		t.Fatalf("%q %q", s, id)
	}
}
