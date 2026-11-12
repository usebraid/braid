// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package parse turns raw transcript lines and snapshots into typed events, leniently and without I/O.
package parse

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// FileKind is the kind of a transcript-tree file.
type FileKind uint8

// File kinds understood by ClassifyPath.
const (
	FileOther FileKind = iota
	FileMain
	FileSubagent
	FileAgentMeta
	FileCustomTitle
)

// FileInfo is the result of ClassifyPath.
type FileInfo struct {
	Kind               FileKind
	SessionID, AgentID string
}

// ClassifyPath classifies a path relative to the projects root.
func ClassifyPath(rel string) FileInfo {
	seg := strings.Split(filepath.ToSlash(rel), "/")
	switch len(seg) {
	case 2:
		if sid, ok := strings.CutSuffix(seg[1], ".jsonl"); ok && sid != "" && seg[0] != "" {
			return FileInfo{FileMain, sid, "main"}
		}
	case 3:
		if seg[2] == "custom-title.json" && seg[0] != "" && seg[1] != "" {
			return FileInfo{FileCustomTitle, seg[1], "main"}
		}
	case 4:
		if seg[0] == "" || seg[1] == "" || seg[2] != "subagents" {
			break
		}
		rest, ok := strings.CutPrefix(seg[3], "agent-")
		if !ok {
			break
		}
		if id, ok := strings.CutSuffix(rest, ".meta.json"); ok && id != "" {
			return FileInfo{FileAgentMeta, seg[1], id}
		}
		if id, ok := strings.CutSuffix(rest, ".jsonl"); ok && id != "" {
			return FileInfo{FileSubagent, seg[1], id}
		}
	}
	return FileInfo{}
}

// Kind is the type of a parsed Event.
type Kind uint8

// Event kinds.
const (
	KindTouch Kind = iota + 1
	KindPrompt
	KindAssistant
	KindToolUse
	KindToolResult
	KindTurn
	KindTitle
	KindTaskNote
	KindFailure
	KindAgentMeta
	KindCustomTitle
)

// Usage is the token usage of one assistant message.
type Usage struct {
	ID, Model, Speed                                               string
	Input, Output, CacheRead, CacheWrite5m, CacheWrite1h, Thinking int64
	WebSearch, WebFetch                                            int64
}

// Failure is a classified API, refusal or tool failure.
type Failure struct {
	Category, Detail, LimitType string
	ResetsAt                    time.Time // zero if none
}

// AgentMeta is the content of an agent-*.meta.json snapshot.
type AgentMeta struct {
	AgentType, Description, ToolUseID, ParentAgentID string
	SpawnDepth                                       int
	Finished                                         *bool
}

// Event is one typed fact extracted from a line or snapshot; which fields are set depends on Kind.
type Event struct {
	Kind                                    Kind
	TS                                      time.Time // zero if the line has none
	CWD, Branch                             string
	Sidechain                               bool
	Text                                    string // capped at MaxText, cut on a rune boundary
	AI                                      bool
	Model                                   string
	Usage                                   *Usage
	ToolUseID, Tool, Summary, Skill, Idents string
	IsError                                 bool
	DurationMs, AgentTotalMs                *int64
	Failure                                 *Failure
	Meta                                    *AgentMeta
}

// MaxText caps Event.Text in bytes.
const MaxText = 64 << 10

// ErrBadLine is returned for input that is not valid JSON.
var ErrBadLine = errors.New("parse: line is not valid JSON")

type obj = map[string]any

// Lenient accessors: a missing field or one of the wrong JSON type reads as the zero value.
func str(m obj, k string) string { s, _ := m[k].(string); return s }
func sub(m obj, k string) obj    { o, _ := m[k].(obj); return o }
func arr(m obj, k string) []any  { a, _ := m[k].([]any); return a }
func boolean(m obj, k string) bool {
	b, _ := m[k].(bool)
	return b
}
func num(m obj, k string) *int64 {
	f, ok := m[k].(float64)
	if !ok {
		return nil
	}
	n := int64(f)
	return &n
}
func num0(m obj, k string) int64 {
	if n := num(m, k); n != nil {
		return *n
	}
	return 0
}

func capText(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	s = s[:limit]
	// drop a trailing partial rune
	for len(s) > 0 {
		r, size := utf8.DecodeLastRuneInString(s)
		if r == utf8.RuneError && size <= 1 {
			s = s[:len(s)-1]
			continue
		}
		break
	}
	return s
}

var (
	reToolUseID = regexp.MustCompile(`<tool-use-id>([^<]*)</tool-use-id>`)
	reDuration  = regexp.MustCompile(`<duration_ms>(\d+)</duration_ms>`)
)

func decode(raw []byte) (obj, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, ErrBadLine
	}
	m, _ := v.(obj)
	return m, nil
}

// ParseLine returns the events of one raw line, in document order (Touch first).
//
//nolint:revive // name fixed by the task interface
func ParseLine(raw []byte) ([]Event, error) {
	m, err := decode(raw)
	if err != nil || m == nil {
		return nil, err
	}
	var base Event
	if ts, err := time.Parse(time.RFC3339Nano, str(m, "timestamp")); err == nil {
		base.TS = ts
	}
	base.CWD, base.Branch, base.Sidechain = str(m, "cwd"), str(m, "gitBranch"), boolean(m, "isSidechain")
	var evs []Event
	add := func(e Event) {
		e.TS, e.CWD, e.Branch, e.Sidechain = base.TS, base.CWD, base.Branch, base.Sidechain
		evs = append(evs, e)
	}
	if !base.TS.IsZero() {
		add(Event{Kind: KindTouch})
	}
	switch str(m, "type") {
	case "user":
		parseUser(m, add)
	case "assistant":
		parseAssistant(m, add)
	case "system":
		sd := str(m, "subtype")
		switch {
		case sd == "turn_duration":
			add(Event{Kind: KindTurn, DurationMs: num(m, "durationMs")})
		case strings.HasPrefix(sd, "model_refusal"):
			d := sd
			if c := str(m, "apiRefusalCategory"); c != "" {
				d += "/" + c
			}
			add(Event{Kind: KindFailure, Failure: &Failure{Category: "refusal", Detail: d}})
		}
	case "ai-title":
		add(Event{Kind: KindTitle, AI: true, Text: capText(str(m, "aiTitle"), MaxText)})
	case "custom-title":
		add(Event{Kind: KindTitle, Text: capText(str(m, "customTitle"), MaxText)})
	case "queue-operation":
		c := str(m, "content")
		if str(m, "operation") == "enqueue" && strings.Contains(c, "<task-notification>") {
			id, d := reToolUseID.FindStringSubmatch(c), reDuration.FindStringSubmatch(c)
			if id != nil && d != nil {
				if n, err := strconv.ParseInt(d[1], 10, 64); err == nil {
					add(Event{Kind: KindTaskNote, ToolUseID: id[1], AgentTotalMs: &n})
				}
			}
		}
	}
	return evs, nil
}

func parseUser(m obj, add func(Event)) {
	msg := sub(m, "message")
	content := msg["content"]
	blocks, _ := content.([]any)
	tur := sub(m, "toolUseResult")
	hasResult, anyErr := false, false
	var text []string
	for _, b := range blocks {
		blk, _ := b.(obj)
		switch str(blk, "type") {
		case "tool_result":
			hasResult = true
			e := Event{Kind: KindToolResult, ToolUseID: str(blk, "tool_use_id"), IsError: boolean(blk, "is_error")}
			if tur != nil {
				e.DurationMs, e.AgentTotalMs = num(tur, "durationMs"), num(tur, "totalDurationMs")
			}
			anyErr = anyErr || e.IsError
			add(e)
		case "text":
			text = append(text, str(blk, "text"))
		}
	}
	if hasResult {
		if anyErr {
			d := str(m, "toolDenialKind")
			if d == "" {
				d = "error"
			}
			add(Event{Kind: KindFailure, Failure: &Failure{Category: "tool_error", Detail: d}})
		}
		return
	}
	if boolean(m, "isSidechain") || boolean(m, "isMeta") {
		return
	}
	if k := str(sub(m, "origin"), "kind"); k != "" && k != "human" {
		return
	}
	s, ok := content.(string)
	if !ok {
		s = strings.Join(text, "\n")
	}
	add(Event{Kind: KindPrompt, Text: capText(s, MaxText)})
}

func parseAssistant(m obj, add func(Event)) {
	if boolean(m, "isApiErrorMessage") {
		add(Event{Kind: KindFailure, Failure: classifyAPIError(m)})
		return
	}
	msg := sub(m, "message")
	var text []string
	var uses []Event
	for _, b := range arr(msg, "content") {
		blk, _ := b.(obj)
		switch str(blk, "type") {
		case "text":
			text = append(text, str(blk, "text"))
		case "tool_use":
			in := sub(blk, "input")
			tool := str(blk, "name")
			e := Event{Kind: KindToolUse, ToolUseID: str(blk, "id"), Tool: tool}
			e.Summary, e.Idents = Summarize(tool, in)
			if tool == "Skill" {
				e.Skill = str(in, "skill")
			}
			uses = append(uses, e)
		}
	}
	model := str(msg, "model")
	a := Event{Kind: KindAssistant, Model: model, Text: capText(strings.Join(text, "\n"), MaxText)}
	if u, id := sub(msg, "usage"), str(msg, "id"); u != nil && id != "" && model != "<synthetic>" {
		us := &Usage{
			ID: id, Model: model, Speed: str(u, "speed"),
			Input: num0(u, "input_tokens"), Output: num0(u, "output_tokens"), CacheRead: num0(u, "cache_read_input_tokens"),
			Thinking:  num0(sub(u, "output_tokens_details"), "thinking_tokens"),
			WebSearch: num0(sub(u, "server_tool_use"), "web_search_requests"),
			WebFetch:  num0(sub(u, "server_tool_use"), "web_fetch_requests"),
		}
		if cc := sub(u, "cache_creation"); cc["ephemeral_5m_input_tokens"] != nil || cc["ephemeral_1h_input_tokens"] != nil {
			us.CacheWrite5m, us.CacheWrite1h = num0(cc, "ephemeral_5m_input_tokens"), num0(cc, "ephemeral_1h_input_tokens")
		} else {
			us.CacheWrite5m = num0(u, "cache_creation_input_tokens")
		}
		a.Usage = us
	}
	add(a)
	for _, e := range uses {
		add(e)
	}
}

func classifyAPIError(m obj) *Failure {
	apiErr, errS := str(m, "apiError"), str(m, "error")
	status := num(m, "apiErrorStatus")
	f := &Failure{}
	var parts []string
	for _, p := range []string{errS, apiErr} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if status != nil {
		parts = append(parts, strconv.FormatInt(*status, 10))
	}
	f.Detail = strings.Join(parts, "/")
	low := strings.ToLower(errS + " " + apiErr)
	ql := sub(m, "quotaLimits")
	switch {
	case ql != nil || apiErr == "usage_limit_reached":
		f.Category = "usage_limit"
		rl := sub(sub(m, "apiErrorParams"), "rate_limit_info")
		f.LimitType = str(ql, "rateLimitType")
		if f.LimitType == "" {
			f.LimitType = str(rl, "rateLimitType")
		}
		secs := num(ql, "resetsAt")
		if secs == nil {
			secs = num(rl, "resetsAt")
		}
		if secs != nil {
			f.ResetsAt = time.Unix(*secs, 0).UTC()
		}
	case apiErr == "computer_slept" || containsAny(low, "connection", "network", "timeout", "econn"):
		f.Category = "network"
	case errS == "rate_limit" || (status != nil && *status == 429):
		f.Category = "rate_limit"
	case errS == "server_error" || errS == "overloaded" || strings.Contains(apiErr, "overloaded") || (status != nil && *status >= 500):
		f.Category = "server_error"
	default:
		f.Category = "other_api_error"
	}
	return f
}

func containsAny(s string, subs ...string) bool {
	for _, p := range subs {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

// ParseSnapshot handles FileAgentMeta and FileCustomTitle snapshots; other kinds yield nil.
//
//nolint:revive // name fixed by the task interface
func ParseSnapshot(fi FileInfo, content []byte) ([]Event, error) {
	if fi.Kind != FileAgentMeta && fi.Kind != FileCustomTitle {
		return nil, nil
	}
	m, err := decode(content)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, ErrBadLine
	}
	if fi.Kind == FileCustomTitle {
		return []Event{{Kind: KindCustomTitle, Text: capText(str(m, "customTitle"), MaxText)}}, nil
	}
	am := &AgentMeta{
		AgentType: str(m, "agentType"), Description: str(m, "description"), ToolUseID: str(m, "toolUseId"),
		ParentAgentID: str(m, "parentAgentId"), SpawnDepth: int(num0(m, "spawnDepth")),
	}
	if b, ok := m["finished"].(bool); ok {
		am.Finished = &b
	}
	return []Event{{Kind: KindAgentMeta, Meta: am}}, nil
}

var summaryField = map[string][]string{
	"Bash": {"command"}, "Read": {"file_path", "notebook_path"}, "Edit": {"file_path", "notebook_path"},
	"Write": {"file_path", "notebook_path"}, "NotebookEdit": {"notebook_path", "file_path"},
	"Grep": {"pattern"}, "Glob": {"pattern"}, "Skill": {"skill"}, "Agent": {"description"},
	"WebFetch": {"url"}, "WebSearch": {"query"},
}

var genericFields = []string{"file_path", "path", "command", "pattern", "description", "url", "query"}

// Summarize returns the <=200-rune input summary and the idents string of a tool call.
// Only the whitelisted fields are ever read, never content/old_string/new_string/text/prompt.
func Summarize(tool string, input map[string]any) (summary, idents string) {
	fields, ok := summaryField[tool]
	if !ok {
		fields = genericFields
	}
	for _, f := range fields {
		if s := str(input, f); s != "" {
			summary = s
			break
		}
	}
	if r := []rune(summary); len(r) > 200 {
		summary = string(r[:200])
	}
	idents = tool
	for _, f := range []string{"file_path", "path", "notebook_path"} {
		if s := str(input, f); s != "" {
			idents += " " + s
			break
		}
	}
	return summary, capText(idents, 500)
}
