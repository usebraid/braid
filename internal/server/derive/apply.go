// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package derive

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/usebraid/braid/internal/server/derive/parse"
)

type projAcc struct{ first, last time.Time }

type toolAcc struct {
	name, skill, summary string
	started, ended       time.Time
	isErr                bool
	resultMs, totalMs    *int64
}

type failureAcc struct {
	f      *parse.Failure
	at     time.Time
	offset int64
}

type docAcc struct {
	role, text, tools, idents string
	at                        time.Time
	offset                    int64
}

// eventAcc is a session event (start or resume) found in this batch.
type eventAcc struct {
	kind, device, model string
	at                  time.Time
	offset              int64
}

type orderRow struct {
	id int64
	ts *time.Time // nil: no timestamp known yet (stored as -infinity until the file's first one shows up)
}

type usageAcc struct {
	u      *parse.Usage
	at     time.Time
	offset int64
}

// batch holds the in-memory deltas of one chunk; a chunk is always one file, hence one session and one agent.
type batch struct {
	userID, session, agent          string
	fileID                          int64
	main, sub, metaFile             bool
	lines, bad, tooLarge, badOffset int64
	maxTS                           time.Time
	devices                         map[string]int64
	projects                        map[string]*projAcc
	tools                           map[string]*toolAcc
	usage                           map[string]*usageAcc
	models                          map[string]bool
	failures                        map[int64]*failureAcc
	docs                            map[int64]*docAcc
	// session and agent
	started, ended                time.Time
	projPath, branch, firstPrompt string
	projAt, branchAt, promptAt    time.Time
	aiTitle, customTitle          *string
	meta                          *parse.AgentMeta
	// agent time
	activeMs, activeTurns int64       // deltas of this batch
	at                    activeState // running state, seeded from the agent row
	prevTS                time.Time   // newest Touch so far, seeded from the cursor (failure timestamps)
	// graph (main file only: events; main and subagent files: line order)
	start             *eventAcc
	resumes, open     []*eventAcc // open: events still waiting for the model of the next assistant line
	turnDevice        string      // device of the last turn start, seeded from agents.last_turn_device
	asstModel         string      // model of the first assistant line of this batch
	asstOffset        int64
	order             []orderRow
	lineTS, lineFirst *time.Time // lineTS: ts carried to lines without one, seeded from line_order; lineFirst: first ts of a file that had none
	lineSeeded        bool
}

// activeState is the open turn of an agent (see agents.work_prev, pending_ms, turn_asst).
type activeState struct {
	prev    time.Time // zero: no turn started yet
	pending int64     // ms of the open turn before its first assistant line
	asst    bool
}

// maxGap is the longest gap that counts in full; a longer one counts only as far as the tool result reports.
const maxGap = 30 * time.Minute

// activeTime applies one line to the agent's active time (spec 02 section 4).
func (b *batch) activeTime(e parse.Event) {
	s := &b.at
	if e.TurnStart {
		*s = activeState{prev: e.TS}
		return
	}
	if !e.Work || s.prev.IsZero() {
		return
	}
	var ms int64
	if gap := e.TS.Sub(s.prev); gap > maxGap {
		if e.ReportedMs != nil {
			ms = min(gap.Milliseconds(), max(*e.ReportedMs, 0))
		}
	} else if gap > 0 {
		ms = gap.Milliseconds()
	}
	if e.TS.After(s.prev) {
		s.prev = e.TS
	}
	if e.WorkAsst && !s.asst { // a turn counts once it has an assistant line
		s.asst = true
		b.activeTurns++
		b.activeMs += s.pending
		s.pending = 0
	}
	if s.asst {
		b.activeMs += ms
	} else {
		s.pending += ms
	}
}

// graphEvent records the start and resume events of one Touch line of the main transcript.
// It must run before activeTime, which resets the work_prev it reads.
func (b *batch) graphEvent(e parse.Event, row rawRow) {
	if b.start == nil || e.TS.Before(b.start.at) {
		s := &eventAcc{kind: "start", at: e.TS, device: row.device, offset: row.offset}
		b.start, b.open = s, append(b.open, s) // its model is the first reply after its line, not that of the start it replaces
	}
	if !e.TurnStart || e.Sidechain {
		return
	}
	if (!b.at.prev.IsZero() && e.TS.Sub(b.at.prev) > maxGap) || (b.turnDevice != "" && b.turnDevice != row.device) {
		r := &eventAcc{kind: "resume", at: e.TS, device: row.device, offset: row.offset}
		b.resumes, b.open = append(b.resumes, r), append(b.open, r)
	}
	b.turnDevice = row.device
}

// lineOrder adds the line_order row of a raw line; evs is nil for a line that did not parse.
func (b *batch) lineOrder(id int64, evs []parse.Event) {
	if !b.main && !b.sub {
		return
	}
	if len(evs) > 0 && !evs[0].TS.IsZero() { // every event of a line carries the line's timestamp
		ts := evs[0].TS
		b.lineTS = &ts
		if !b.lineSeeded && b.lineFirst == nil {
			b.lineFirst = &ts
		}
	}
	b.order = append(b.order, orderRow{id, b.lineTS})
}

func newBatch(userID string, fileID int64, fi parse.FileInfo) *batch {
	return &batch{userID: userID, fileID: fileID, session: fi.SessionID, agent: fi.AgentID, main: fi.Kind == parse.FileMain, sub: fi.Kind == parse.FileSubagent, metaFile: fi.Kind == parse.FileAgentMeta,
		devices: map[string]int64{}, projects: map[string]*projAcc{}, tools: map[string]*toolAcc{}, usage: map[string]*usageAcc{}, models: map[string]bool{}, failures: map[int64]*failureAcc{}, docs: map[int64]*docAcc{}}
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func earlier(a, b time.Time) bool { return a.IsZero() || b.Before(a) }
func later(a, b time.Time) bool   { return a.IsZero() || !b.Before(a) }

func (b *batch) tool(id string) *toolAcc {
	t := b.tools[id]
	if t == nil {
		t = &toolAcc{}
		b.tools[id] = t
	}
	return t
}

func (b *batch) add(evs []parse.Event, row rawRow) {
	offset := row.offset
	doc := &docAcc{offset: offset} // snapshots yield no prompt/assistant events, so they never produce a doc
	defer func() {
		if doc.text != "" || doc.tools != "" {
			b.docs[offset] = doc
		}
	}()
	for _, e := range evs {
		if later(b.maxTS, e.TS) && !e.TS.IsZero() {
			b.maxTS = e.TS
		}
		switch e.Kind {
		case parse.KindTouch:
			if b.main {
				b.graphEvent(e, row)
			}
			b.activeTime(e)
			if later(b.prevTS, e.TS) {
				b.prevTS = e.TS
			}
			if earlier(b.started, e.TS) {
				b.started = e.TS
			}
			if later(b.ended, e.TS) {
				b.ended = e.TS
			}
			if e.Sidechain {
				break
			}
			if e.CWD != "" {
				p := b.projects[e.CWD]
				if p == nil {
					b.projects[e.CWD] = &projAcc{e.TS, e.TS}
				} else {
					if e.TS.Before(p.first) {
						p.first = e.TS
					}
					if e.TS.After(p.last) {
						p.last = e.TS
					}
				}
				if earlier(b.projAt, e.TS) {
					b.projPath, b.projAt = e.CWD, e.TS
				}
			}
			if e.Branch != "" && later(b.branchAt, e.TS) {
				b.branch, b.branchAt = e.Branch, e.TS
			}
		case parse.KindFailure:
			at := e.TS
			if at.IsZero() {
				at = b.prevTS // newest ts seen so far, seeded from the cursor
			}
			if !at.IsZero() {
				b.failures[offset] = &failureAcc{e.Failure, at, offset}
			}
		case parse.KindPrompt:
			doc.role, doc.text, doc.at = "user", e.Text, e.TS
			if !e.TS.IsZero() && earlier(b.promptAt, e.TS) {
				b.firstPrompt, b.promptAt = firstPromptText(e.Text), e.TS
			}
		case parse.KindTitle:
			s := e.Text
			if e.AI {
				b.aiTitle = &s
			} else {
				b.customTitle = &s
			}
		case parse.KindCustomTitle:
			s := e.Text
			b.customTitle = &s
		case parse.KindAgentMeta:
			b.meta = e.Meta
		case parse.KindAssistant:
			doc.role, doc.text, doc.at = "assistant", e.Text, e.TS
			if m := NormalizeModel(e.Model); b.main && !e.Sidechain && e.Model != "" {
				for _, ev := range b.open {
					ev.model = m
				}
				if b.open = b.open[:0]; b.asstModel == "" {
					b.asstModel, b.asstOffset = m, offset
				}
			}
			if e.Usage == nil || e.TS.IsZero() {
				break
			}
			b.models[NormalizeModel(e.Usage.Model)] = true
			if cur := b.usage[e.Usage.ID]; cur == nil || e.Usage.Output > cur.u.Output || (e.Usage.Output == cur.u.Output && offset < cur.offset) {
				b.usage[e.Usage.ID] = &usageAcc{e.Usage, e.TS, offset}
			}
		case parse.KindToolUse:
			if doc.role == "" {
				doc.role = "assistant"
			}
			doc.tools, doc.idents = strings.TrimSpace(doc.tools+" "+e.Tool), strings.TrimSpace(doc.idents+" "+e.Idents)
			t := b.tool(e.ToolUseID)
			t.name, t.skill, t.summary = e.Tool, e.Skill, e.Summary
			if !e.TS.IsZero() && earlier(t.started, e.TS) {
				t.started = e.TS
			}
		case parse.KindTaskNote:
			b.tool(e.ToolUseID).totalMs = e.AgentTotalMs
		case parse.KindToolResult:
			t := b.tool(e.ToolUseID)
			if later(t.ended, e.TS) && !e.TS.IsZero() {
				t.ended = e.TS
			}
			t.isErr = t.isErr || e.IsError
			if e.DurationMs != nil {
				t.resultMs = e.DurationMs
			}
			if e.AgentTotalMs != nil {
				t.totalMs = e.AgentTotalMs
			}
		}
	}
}

func firstPromptText(s string) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) > 80 {
		r = r[:80]
	}
	return string(r)
}

func (b *batch) flush(ctx context.Context, tx pgx.Tx) error {
	u := b.userID
	exec := func(q string, args ...any) error {
		_, err := tx.Exec(ctx, q, append([]any{u}, stripNULArgs(args)...)...)
		return err
	}

	if len(b.projects) > 0 {
		var paths, names []string
		var first, last []time.Time
		for p, a := range b.projects {
			paths, names, first, last = append(paths, p), append(names, projectName(p)), append(first, a.first), append(last, a.last)
		}
		if err := exec(`INSERT INTO projects(user_id, path, name, first_seen_at, last_seen_at)
		  SELECT $1, p, n, f, l FROM unnest($2::text[], $3::text[], $4::timestamptz[], $5::timestamptz[]) AS x(p, n, f, l)
		  ON CONFLICT (user_id, path) DO UPDATE SET name = EXCLUDED.name,
		    first_seen_at = least(projects.first_seen_at, EXCLUDED.first_seen_at), last_seen_at = greatest(projects.last_seen_at, EXCLUDED.last_seen_at)`,
			paths, names, first, last); err != nil {
			return err
		}
	}

	if b.session != "" && !b.metaFile {
		models := make([]string, 0, len(b.models))
		for m := range b.models {
			models = append(models, m)
		}
		slices.Sort(models)
		sessLines := b.lines // snapshots are not lines
		if err := exec(`INSERT INTO sessions(user_id, session_id, started_at, ended_at, project_path, project_at, git_branch, git_branch_at,
		    models, first_prompt, first_prompt_at, ai_title, custom_title, line_count)
		  VALUES ($1, $2, $3, $4, nullif($5, ''), $6, nullif($7, ''), $8, $9, nullif($10, ''), $11, $12, $13, $14)
		  ON CONFLICT (user_id, session_id) DO UPDATE SET
		    started_at = least(sessions.started_at, EXCLUDED.started_at), ended_at = greatest(sessions.ended_at, EXCLUDED.ended_at),
		    project_path = CASE WHEN EXCLUDED.project_at IS NOT NULL AND (sessions.project_at IS NULL OR EXCLUDED.project_at < sessions.project_at) THEN EXCLUDED.project_path ELSE sessions.project_path END,
		    project_at = CASE WHEN EXCLUDED.project_at IS NOT NULL AND (sessions.project_at IS NULL OR EXCLUDED.project_at < sessions.project_at) THEN EXCLUDED.project_at ELSE sessions.project_at END,
		    git_branch = CASE WHEN EXCLUDED.git_branch_at IS NOT NULL AND (sessions.git_branch_at IS NULL OR EXCLUDED.git_branch_at >= sessions.git_branch_at) THEN EXCLUDED.git_branch ELSE sessions.git_branch END,
		    git_branch_at = CASE WHEN EXCLUDED.git_branch_at IS NOT NULL AND (sessions.git_branch_at IS NULL OR EXCLUDED.git_branch_at >= sessions.git_branch_at) THEN EXCLUDED.git_branch_at ELSE sessions.git_branch_at END,
		    models = (SELECT coalesce(array_agg(DISTINCT m ORDER BY m), '{}') FROM unnest(sessions.models || EXCLUDED.models) m),
		    first_prompt = CASE WHEN EXCLUDED.first_prompt_at IS NOT NULL AND (sessions.first_prompt_at IS NULL OR EXCLUDED.first_prompt_at < sessions.first_prompt_at) THEN EXCLUDED.first_prompt ELSE sessions.first_prompt END,
		    first_prompt_at = CASE WHEN EXCLUDED.first_prompt_at IS NOT NULL AND (sessions.first_prompt_at IS NULL OR EXCLUDED.first_prompt_at < sessions.first_prompt_at) THEN EXCLUDED.first_prompt_at ELSE sessions.first_prompt_at END,
		    ai_title = coalesce(EXCLUDED.ai_title, sessions.ai_title), custom_title = coalesce(EXCLUDED.custom_title, sessions.custom_title),
		    line_count = sessions.line_count + EXCLUDED.line_count`, // shortcut: rows re-sent at an existing offset or a new head generation are counted again (also turn_ms, session_devices.line_count); derive only the newest row per (file, offset) once the spec decides
			b.session, nullTime(b.started), nullTime(b.ended), b.projPath, nullTime(b.projAt), b.branch, nullTime(b.branchAt),
			models, b.firstPrompt, nullTime(b.promptAt), b.aiTitle, b.customTitle, sessLines); err != nil {
			return err
		}
		if len(b.devices) > 0 {
			var ds []string
			var cs []int64
			for d, c := range b.devices {
				ds, cs = append(ds, d), append(cs, c)
			}
			if err := exec(`INSERT INTO session_devices(user_id, session_id, device_id, line_count)
			  SELECT $1, $2, d::uuid, c FROM unnest($3::text[], $4::bigint[]) AS x(d, c)
			  ON CONFLICT (user_id, session_id, device_id) DO UPDATE SET line_count = session_devices.line_count + EXCLUDED.line_count`,
				b.session, ds, cs); err != nil {
				return err
			}
			if err := exec(`UPDATE sessions SET primary_device_id = (SELECT d.device_id FROM session_devices d
			    WHERE d.user_id = sessions.user_id AND d.session_id = sessions.session_id ORDER BY d.line_count DESC, d.device_id LIMIT 1)
			  WHERE user_id = $1 AND session_id = $2`, b.session); err != nil {
				return err
			}
		}
	}

	if b.session != "" && b.agent != "" {
		agentType := (*string)(nil)
		if b.main {
			m := "main"
			agentType = &m
		}
		if b.meta != nil || b.main || b.sub {
			m := b.meta
			if m == nil {
				m = &parse.AgentMeta{}
			}
			if err := exec(`INSERT INTO agents(user_id, session_id, agent_id, agent_type, description, parent_hint, spawn_tool_use_id, spawn_depth, finished, started_at, ended_at,
			    active_ms, turn_count, work_prev, pending_ms, turn_asst, last_turn_device)
			  VALUES ($1, $2, $3, coalesce(nullif($4::text, ''), $12::text), nullif($5, ''), nullif($6, ''), nullif($7, ''), $8, $9, $10, $11, $14, $15, $16, $17, $18, nullif($20, '')::uuid)
			  ON CONFLICT (user_id, session_id, agent_id) DO UPDATE SET
			    agent_type = coalesce(EXCLUDED.agent_type, agents.agent_type), description = coalesce(EXCLUDED.description, agents.description),
			    parent_hint = coalesce(EXCLUDED.parent_hint, agents.parent_hint), spawn_tool_use_id = coalesce(EXCLUDED.spawn_tool_use_id, agents.spawn_tool_use_id),
			    spawn_depth = CASE WHEN $13 THEN EXCLUDED.spawn_depth ELSE agents.spawn_depth END, finished = coalesce(EXCLUDED.finished, agents.finished),
			    started_at = least(agents.started_at, EXCLUDED.started_at), ended_at = greatest(agents.ended_at, EXCLUDED.ended_at),
			    active_ms = agents.active_ms + EXCLUDED.active_ms, turn_count = agents.turn_count + EXCLUDED.turn_count,
			    work_prev = CASE WHEN $19 THEN EXCLUDED.work_prev ELSE agents.work_prev END, pending_ms = CASE WHEN $19 THEN EXCLUDED.pending_ms ELSE agents.pending_ms END,
			    turn_asst = CASE WHEN $19 THEN EXCLUDED.turn_asst ELSE agents.turn_asst END,
			    last_turn_device = CASE WHEN $19 THEN EXCLUDED.last_turn_device ELSE agents.last_turn_device END`,
				b.session, b.agent, m.AgentType, m.Description, m.ParentAgentID, m.ToolUseID, m.SpawnDepth, m.Finished,
				nullTime(b.started), nullTime(b.ended), agentType, b.meta != nil, b.activeMs, b.activeTurns, nullTime(b.at.prev), b.at.pending, b.at.asst, b.main || b.sub, b.turnDevice); err != nil {
				return err
			}
		}
	}

	if err := b.flushGraph(ctx, tx); err != nil {
		return err
	}

	if len(b.tools) > 0 {
		var ids, names, skills, sums []string
		var started, ended []*time.Time
		var errs []bool
		var res, tot []*int64
		for id, t := range b.tools {
			ids, names, skills, sums = append(ids, id), append(names, t.name), append(skills, t.skill), append(sums, t.summary)
			started, ended, errs, res, tot = append(started, nullTime(t.started)), append(ended, nullTime(t.ended)), append(errs, t.isErr), append(res, t.resultMs), append(tot, t.totalMs)
		}
		if err := exec(`INSERT INTO tool_calls(user_id, session_id, tool_use_id, agent_id, tool_name, skill, input_summary, started_at, ended_at, is_error, result_ms, agent_total_ms)
		  SELECT $1, $2, i, $3, n, nullif(k, ''), s, st, en, e, r, t FROM unnest($4::text[], $5::text[], $6::text[], $7::text[], $8::timestamptz[], $9::timestamptz[], $10::bool[], $11::bigint[], $12::bigint[])
		    AS x(i, n, k, s, st, en, e, r, t)
		  ON CONFLICT (user_id, session_id, tool_use_id) DO UPDATE SET
		    tool_name = CASE WHEN EXCLUDED.tool_name <> '' THEN EXCLUDED.tool_name ELSE tool_calls.tool_name END,
		    skill = CASE WHEN EXCLUDED.tool_name <> '' THEN EXCLUDED.skill ELSE tool_calls.skill END,
		    input_summary = CASE WHEN EXCLUDED.tool_name <> '' THEN EXCLUDED.input_summary ELSE tool_calls.input_summary END,
		    agent_id = CASE WHEN EXCLUDED.tool_name <> '' THEN EXCLUDED.agent_id ELSE tool_calls.agent_id END,
		    started_at = least(tool_calls.started_at, EXCLUDED.started_at), ended_at = greatest(tool_calls.ended_at, EXCLUDED.ended_at),
		    is_error = tool_calls.is_error OR EXCLUDED.is_error,
		    result_ms = coalesce(EXCLUDED.result_ms, tool_calls.result_ms), agent_total_ms = coalesce(EXCLUDED.agent_total_ms, tool_calls.agent_total_ms)`,
			b.session, b.agent, ids, names, skills, sums, started, ended, errs, res, tot); err != nil {
			return err
		}
	}

	if len(b.usage) > 0 {
		var ids, models, speeds []string
		var at []time.Time
		var in, out, cr, w5, w1, th, ws, wf, off []int64
		var cost []float64
		var unk, est []bool
		for id, a := range b.usage {
			c, estimated, unknown := Cost(*a.u)
			ids, models, speeds, at = append(ids, id), append(models, NormalizeModel(a.u.Model)), append(speeds, a.u.Speed), append(at, a.at)
			in, out, cr, w5, w1 = append(in, a.u.Input), append(out, a.u.Output), append(cr, a.u.CacheRead), append(w5, a.u.CacheWrite5m), append(w1, a.u.CacheWrite1h)
			th, ws, wf, off = append(th, a.u.Thinking), append(ws, a.u.WebSearch), append(wf, a.u.WebFetch), append(off, a.offset)
			cost, unk, est = append(cost, c), append(unk, unknown), append(est, estimated)
		}
		if err := exec(`INSERT INTO usage(user_id, message_id, session_id, agent_id, model, input_tokens, output_tokens, cache_read_tokens, cache_write_5m_tokens, cache_write_1h_tokens,
		    thinking_tokens, web_search_requests, web_fetch_requests, speed, at, source_file_id, "offset", cost_usd, price_unknown, price_estimated)
		  SELECT $1, i, $2, $3, m, a, o, r, w5, w1, t, ws, wf, sp, at, $4, off, c, u, pe
		    FROM unnest($5::text[], $6::text[], $7::bigint[], $8::bigint[], $9::bigint[], $10::bigint[], $11::bigint[], $12::bigint[], $13::bigint[], $14::bigint[], $15::text[],
		                $16::timestamptz[], $17::bigint[], $18::float8[], $19::bool[], $20::bool[])
		    AS x(i, m, a, o, r, w5, w1, t, ws, wf, sp, at, off, c, u, pe)
		  ON CONFLICT (user_id, message_id) DO UPDATE SET model = EXCLUDED.model, session_id = EXCLUDED.session_id, agent_id = EXCLUDED.agent_id,
		    input_tokens = EXCLUDED.input_tokens, output_tokens = EXCLUDED.output_tokens, cache_read_tokens = EXCLUDED.cache_read_tokens,
		    cache_write_5m_tokens = EXCLUDED.cache_write_5m_tokens, cache_write_1h_tokens = EXCLUDED.cache_write_1h_tokens, thinking_tokens = EXCLUDED.thinking_tokens,
		    web_search_requests = EXCLUDED.web_search_requests, web_fetch_requests = EXCLUDED.web_fetch_requests, speed = EXCLUDED.speed, at = EXCLUDED.at,
		    source_file_id = EXCLUDED.source_file_id, "offset" = EXCLUDED."offset", cost_usd = EXCLUDED.cost_usd, price_unknown = EXCLUDED.price_unknown, price_estimated = EXCLUDED.price_estimated
		  WHERE EXCLUDED.output_tokens > usage.output_tokens
		     OR (EXCLUDED.output_tokens = usage.output_tokens AND (EXCLUDED.source_file_id, EXCLUDED."offset") < (usage.source_file_id, usage."offset"))`,
			b.session, b.agent, b.fileID, ids, models, in, out, cr, w5, w1, th, ws, wf, speeds, at, off, cost, unk, est); err != nil {
			return err
		}
	}

	if b.session != "" && len(b.failures) > 0 {
		var off []int64
		var cat, detail, lt []string
		var at, rs []*time.Time
		for _, a := range b.failures {
			off, cat, detail, lt = append(off, a.offset), append(cat, a.f.Category), append(detail, a.f.Detail), append(lt, a.f.LimitType)
			at, rs = append(at, &a.at), append(rs, nullTime(a.f.ResetsAt))
		}
		if err := exec(`INSERT INTO failures(user_id, source_file_id, "offset", category, detail, occurred_at, session_id, agent_id, limit_type, resets_at)
		  SELECT $1, $2, o, c, d, a, $3, $4, nullif(l, ''), r FROM unnest($5::bigint[], $6::text[], $7::text[], $8::timestamptz[], $9::text[], $10::timestamptz[]) AS x(o, c, d, a, l, r)
		  ON CONFLICT (user_id, source_file_id, "offset") DO UPDATE SET category = EXCLUDED.category, detail = EXCLUDED.detail, occurred_at = EXCLUDED.occurred_at,
		    session_id = EXCLUDED.session_id, agent_id = EXCLUDED.agent_id, limit_type = EXCLUDED.limit_type, resets_at = EXCLUDED.resets_at`,
			b.fileID, b.session, b.agent, off, cat, detail, at, lt, rs); err != nil {
			return err
		}
	}

	if b.session != "" && len(b.docs) > 0 {
		var off []int64
		var role, text, idents []string
		var at []*time.Time
		for _, d := range b.docs {
			// Postgres text cannot hold NUL; idents are capped at 1000 bytes (names and paths are ASCII-ish, cut may split a rune, so trim to valid UTF-8).
			off, role, at = append(off, d.offset), append(role, d.role), append(at, nullTime(d.at))
			text = append(text, strings.ToValidUTF8(strings.ReplaceAll(d.text+" "+d.tools, "\x00", ""), ""))
			idents = append(idents, strings.ToValidUTF8(strings.ReplaceAll(d.idents[:min(len(d.idents), 1000)], "\x00", ""), ""))
		}
		if err := exec(`INSERT INTO search_docs(user_id, source_file_id, "offset", session_id, agent_id, role, at, tsv, idents)
		  SELECT $1, $2, o, $3, $4, r, a, to_tsvector('simple', t), i FROM unnest($5::bigint[], $6::text[], $7::timestamptz[], $8::text[], $9::text[]) AS x(o, r, a, t, i)
		  ON CONFLICT (user_id, source_file_id, "offset") DO UPDATE SET session_id = EXCLUDED.session_id, agent_id = EXCLUDED.agent_id,
		    role = EXCLUDED.role, at = EXCLUDED.at, tsv = EXCLUDED.tsv, idents = EXCLUDED.idents`,
			b.fileID, b.session, b.agent, off, role, at, text, idents); err != nil {
			return err
		}
	}

	if b.session != "" {
		if err := b.fixups(ctx, tx); err != nil {
			return err
		}
	}

	if b.bad > 0 {
		if err := exec(`INSERT INTO derive_errors(user_id, source_file_id, errors, last_offset) VALUES ($1, $2, $3, $4)
		  ON CONFLICT (user_id, source_file_id) DO UPDATE SET errors = derive_errors.errors + EXCLUDED.errors, last_offset = greatest(derive_errors.last_offset, EXCLUDED.last_offset)`,
			b.fileID, b.bad, b.badOffset); err != nil {
			return err
		}
	}
	return nil
}

func (b *batch) flushGraph(ctx context.Context, tx pgx.Tx) error {
	u := b.userID
	if b.session != "" && b.main {
		if s := b.start; s != nil {
			// one start per session: an earlier one replaces the stored one, an equal one keeps it (the first row wins, as in a reindex)
			if _, err := tx.Exec(ctx, `DELETE FROM session_events WHERE user_id = $1 AND session_id = $2 AND kind = 'start' AND at > $3`, u, b.session, s.at); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO session_events(user_id, session_id, kind, at, device_id, model, source_file_id, "offset")
			  SELECT $1, $2, 'start', $3, nullif($4, '')::uuid, nullif($5, ''), $6, $7
			   WHERE NOT EXISTS (SELECT 1 FROM session_events WHERE user_id = $1 AND session_id = $2 AND kind = 'start' AND at <= $3)
			  ON CONFLICT DO NOTHING`, u, b.session, s.at, s.device, stripNUL(s.model), b.fileID, s.offset); err != nil {
				return err
			}
		}
		for _, r := range b.resumes {
			if _, err := tx.Exec(ctx, `INSERT INTO session_events(user_id, session_id, kind, at, device_id, model, source_file_id, "offset")
			  VALUES ($1, $2, 'resume', $3, nullif($4, '')::uuid, nullif($5, ''), $6, $7) ON CONFLICT DO NOTHING`,
				u, b.session, r.at, r.device, stripNUL(r.model), b.fileID, r.offset); err != nil {
				return err
			}
		}
	}
	if b.session == "" || len(b.order) == 0 {
		return nil
	}
	ids, ts := make([]int64, len(b.order)), make([]*time.Time, len(b.order))
	for i, o := range b.order {
		ids[i], ts[i] = o.id, o.ts
	}
	if _, err := tx.Exec(ctx, `INSERT INTO line_order(user_id, raw_line_id, session_id, agent_id, ts)
	  SELECT $1, i, $2, $3, coalesce(t, '-infinity') FROM unnest($4::bigint[], $5::timestamptz[]) AS x(i, t)
	  ON CONFLICT (user_id, raw_line_id) DO UPDATE SET session_id = EXCLUDED.session_id, agent_id = EXCLUDED.agent_id, ts = EXCLUDED.ts`,
		u, b.session, stripNUL(b.agent), ids, ts); err != nil {
		return err
	}
	if b.lineFirst != nil { // the leading lines of the file without a timestamp take the file's first one
		_, err := tx.Exec(ctx, `UPDATE line_order o SET ts = $3 FROM raw_lines l
		  WHERE o.user_id = $1 AND l.id = o.raw_line_id AND l.source_file_id = $2 AND o.ts = '-infinity'`, u, b.fileID, *b.lineFirst)
		return err
	}
	return nil
}

// fixups recomputes everything that links rows of different files; idempotent and independent of arrival order.
func (b *batch) fixups(ctx context.Context, tx pgx.Tx) error {
	for _, q := range []string{
		`UPDATE agents a SET result_ms = t.agent_total_ms FROM tool_calls t
		  WHERE a.user_id = $1 AND a.session_id = $2 AND t.user_id = a.user_id AND t.session_id = a.session_id
		    AND t.tool_use_id = a.spawn_tool_use_id AND t.agent_total_ms IS DISTINCT FROM a.result_ms`,
		`UPDATE agents a SET parent_agent_id = CASE WHEN a.spawn_tool_use_id IS NULL THEN a.parent_hint
		    ELSE coalesce((SELECT t.agent_id FROM tool_calls t WHERE t.user_id = a.user_id AND t.session_id = a.session_id AND t.tool_use_id = a.spawn_tool_use_id),
		                  a.parent_hint, 'main') END
		  WHERE a.user_id = $1 AND a.session_id = $2 AND a.agent_id <> 'main'`,
		`DELETE FROM skill_phases WHERE user_id = $1 AND session_id = $2`,
		`INSERT INTO skill_phases(user_id, session_id, agent_id, seq, skill, tool_use_id, started_at, ended_at)
		  SELECT t.user_id, t.session_id, t.agent_id, t.seq, t.skill, t.tool_use_id, t.started_at, coalesce(t.next_start, a.ended_at)
		    FROM (SELECT c.*, row_number() OVER w AS seq, lead(c.started_at) OVER w AS next_start
		            FROM tool_calls c WHERE c.user_id = $1 AND c.session_id = $2 AND c.skill IS NOT NULL AND c.started_at IS NOT NULL
		          WINDOW w AS (PARTITION BY c.agent_id ORDER BY c.started_at, c.tool_use_id)) t
		    LEFT JOIN agents a ON a.user_id = t.user_id AND a.session_id = t.session_id AND a.agent_id = t.agent_id`,
	} {
		if _, err := tx.Exec(ctx, q, b.userID, b.session); err != nil {
			return err
		}
	}
	if b.asstModel == "" {
		return nil
	}
	// events of earlier batches that were still waiting for a reply
	_, err := tx.Exec(ctx, `UPDATE session_events SET model = $3
	  WHERE user_id = $1 AND session_id = $2 AND model IS NULL AND source_file_id = $4 AND "offset" < $5`, b.userID, b.session, stripNUL(b.asstModel), b.fileID, b.asstOffset)
	return err
}

// stripNUL removes U+0000, which Postgres text cannot hold; used for every text value derive writes.
func stripNUL(s string) string { return strings.ReplaceAll(s, "\x00", "") }

func stripNULArgs(args []any) []any {
	out := make([]any, len(args))
	for i, a := range args {
		switch v := a.(type) {
		case string:
			out[i] = stripNUL(v)
		case *string:
			if v != nil {
				c := stripNUL(*v)
				out[i] = &c
			} else {
				out[i] = v
			}
		case []string:
			c := make([]string, len(v))
			for j, x := range v {
				c[j] = stripNUL(x)
			}
			out[i] = c
		default:
			out[i] = a
		}
	}
	return out
}
