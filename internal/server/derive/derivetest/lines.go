// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package derivetest

import (
	"encoding/json"
	"fmt"
	"time"
)

var base = time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)

// At returns the timestamp 2026-11-01T10:00:00.000Z plus sec seconds.
func At(sec int) string {
	return base.Add(time.Duration(sec) * time.Second).Format("2006-01-02T15:04:05.000Z")
}

// Unix returns the epoch seconds of At(sec).
func Unix(sec int) int64 { return base.Unix() + int64(sec) }

func enc(m map[string]any) string {
	b, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// Line builds a base line of the given type; kv pairs override or extend the defaults.
func Line(typ, ts string, kv ...any) string {
	m := map[string]any{"type": typ, "cwd": "/work/alpha", "gitBranch": "main", "isSidechain": false, "sessionId": "s", "version": "2.1.322"}
	if ts != "" {
		m["timestamp"] = ts
	}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return enc(m)
}

func userMsg(content any) map[string]any { return map[string]any{"role": "user", "content": content} }

// Prompt is a typed human prompt.
func Prompt(ts, text string) string {
	return Line("user", ts, "origin", map[string]any{"kind": "human"}, "promptSource", "typed", "message", userMsg(text))
}

// SidePrompt is the first user line of a subagent.
func SidePrompt(ts, text string) string {
	return Line("user", ts, "isSidechain", true, "message", userMsg(text))
}

// Usage builds a usage object.
func Usage(in, out, read, w5, w1 int) map[string]any {
	return map[string]any{"input_tokens": in, "output_tokens": out, "cache_read_input_tokens": read, "cache_creation_input_tokens": w5 + w1,
		"cache_creation": map[string]any{"ephemeral_5m_input_tokens": w5, "ephemeral_1h_input_tokens": w1}, "speed": "standard"}
}

// Reply is an assistant line.
func Reply(ts, msgID, model string, usage map[string]any, blocks ...map[string]any) string {
	if blocks == nil {
		blocks = []map[string]any{}
	}
	return Line("assistant", ts, "message", map[string]any{"id": msgID, "role": "assistant", "model": model, "usage": usage, "content": blocks})
}

// Text is a text content block.
func Text(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

// Tool is a tool_use content block.
func Tool(id, name string, input map[string]any) map[string]any {
	return map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}
}

// Result is a user line with one tool_result block.
func Result(ts, toolUseID string, isError bool, toolUseResult map[string]any) string {
	kv := []any{"message", userMsg([]any{map[string]any{"type": "tool_result", "tool_use_id": toolUseID, "content": "ok", "is_error": isError}})}
	if toolUseResult != nil {
		kv = append(kv, "toolUseResult", toolUseResult)
	}
	return Line("user", ts, kv...)
}

// Turn is a system/turn_duration line.
func Turn(ts string, ms int) string {
	return Line("system", ts, "subtype", "turn_duration", "durationMs", ms)
}

// APIError is a synthetic assistant error line.
func APIError(ts string, kv ...any) string {
	kv = append([]any{"isApiErrorMessage", true, "message", map[string]any{"role": "assistant", "model": "<synthetic>", "content": []any{}}}, kv...)
	return Line("assistant", ts, kv...)
}

// Notify is a queue-operation enqueue carrying a task notification.
func Notify(ts, toolUseID string, ms int) string {
	return Line("queue-operation", ts, "operation", "enqueue",
		"content", fmt.Sprintf("<task-notification><tool-use-id>%s</tool-use-id><duration_ms>%d</duration_ms></task-notification>", toolUseID, ms))
}

func patch(line string, kv ...any) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		panic(err)
	}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return enc(m)
}

// Side marks a line as a subagent line.
func Side(line, agentID string) string { return patch(line, "isSidechain", true, "agentId", agentID) }

// Cwd sets the cwd of a line.
func Cwd(line, cwd string) string { return patch(line, "cwd", cwd) }
