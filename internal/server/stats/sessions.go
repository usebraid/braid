// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package stats

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/usebraid/braid/internal/protocol"
	"github.com/usebraid/braid/internal/server/httpx"
	"github.com/usebraid/braid/internal/server/webauth"
)

// SessionItem is one session with whole-session figures.
type SessionItem struct {
	SessionID        string    `json:"session_id"`
	Title            string    `json:"title"`
	Project          string    `json:"project"`
	ProjectName      string    `json:"project_name"`
	DeviceID         *string   `json:"device_id"`
	DeviceIDs        []string  `json:"device_ids"`
	Models           []string  `json:"models"`
	StartedAt        time.Time `json:"started_at"`
	EndedAt          time.Time `json:"ended_at"`
	IsLive           bool      `json:"is_live"`
	AgentMs          int64     `json:"agent_ms"`
	TotalAgentMs     int64     `json:"total_agent_ms"`
	AgentMsEstimated bool      `json:"agent_ms_estimated"`
	CostUSD          float64   `json:"cost_usd"`
	Tokens           int64     `json:"tokens"`
	Failures         int64     `json:"failures"`
	Skills           []string  `json:"skills"`
	LineCount        int64     `json:"line_count"`
	GitBranch        string    `json:"git_branch"`
	Turns            int64     `json:"turns"`
}

// SessionPage is one page of sessions; Next is the opaque cursor of the following page.
type SessionPage struct {
	Items []SessionItem `json:"items"`
	Next  *string       `json:"next"`
}

// AgentUsage is the usage of one agent.
type AgentUsage struct {
	CostUSD    float64 `json:"cost_usd"`
	Tokens     int64   `json:"tokens"`
	Input      int64   `json:"input"`
	Output     int64   `json:"output"`
	CacheRead  int64   `json:"cache_read"`
	CacheWrite int64   `json:"cache_write"`
	Messages   int64   `json:"messages"`
}

// AgentItem is one agent of a session; the tree is encoded by ParentAgentID.
type AgentItem struct {
	AgentID           string           `json:"agent_id"`
	AgentType         string           `json:"agent_type"`
	Description       string           `json:"description"`
	ParentAgentID     *string          `json:"parent_agent_id"`
	SpawnToolUseID    *string          `json:"spawn_tool_use_id"`
	SpawnDepth        int              `json:"spawn_depth"`
	StartedAt         *time.Time       `json:"started_at"`
	EndedAt           *time.Time       `json:"ended_at"`
	Finished          *bool            `json:"finished"`
	DurationMs        int64            `json:"duration_ms"`
	DurationEstimated bool             `json:"duration_estimated"`
	SpawnedAt         *time.Time       `json:"spawned_at"` // the spawning tool call's start
	MergedAt          *time.Time       `json:"merged_at"`  // its result's time; null while open
	ToolCalls         map[string]int64 `json:"tool_calls"`
	ToolErrors        int64            `json:"tool_errors"`
	Usage             AgentUsage       `json:"usage"`
}

// PhaseItem is one skill phase.
type PhaseItem struct {
	AgentID   string     `json:"agent_id"`
	Seq       int        `json:"seq"`
	Skill     string     `json:"skill"`
	StartedAt *time.Time `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
}

// FailureItem is one failure; TimeLostS is set for usage-limit failures only.
type FailureItem struct {
	Category   string     `json:"category"`
	Detail     string     `json:"detail"`
	OccurredAt time.Time  `json:"occurred_at"`
	SessionID  string     `json:"session_id"`
	AgentID    string     `json:"agent_id"`
	LimitType  *string    `json:"limit_type"`
	ResetsAt   *time.Time `json:"resets_at"`
	TimeLostS  *int64     `json:"time_lost_s"`
	LineID     int64      `json:"line_id"` // raw_lines.id of the failing line (highest id at its offset)
}

// SessionDetail is the response of GET /api/v1/stats/sessions/{id}.
type SessionDetail struct {
	Session     SessionItem   `json:"session"`
	Agents      []AgentItem   `json:"agents"`
	SkillPhases []PhaseItem   `json:"skill_phases"`
	Failures    []FailureItem `json:"failures"`
}

// LineItem is one stored transcript line.
type LineItem struct {
	ID     int64           `json:"id"`
	Offset int64           `json:"offset"`
	Line   json.RawMessage `json:"line"`
}

// LinePage is one page of lines; Next is the last id when more exist.
type LinePage struct {
	Lines []LineItem `json:"lines"`
	Next  *string    `json:"next"`
	Prev  *string    `json:"prev"` // id of the first line when an earlier line of the agent exists
}

// lineIDExpr is the raw_lines.id of failure f; with several rows at one offset (conflict) the highest.
const lineIDExpr = `coalesce((SELECT max(r.id) FROM raw_lines r WHERE r.source_file_id = f.source_file_id AND r."offset" = f."offset"), 0)`

// sessionSelect lists the SessionItem columns; whole-session figures, not clipped to the range.
const sessionSelect = `SELECT s.session_id, coalesce(s.title, ''), coalesce(s.project_path, ''), coalesce(p.name, ''), s.primary_device_id::text,
  coalesce((SELECT array_agg(d.device_id::text ORDER BY d.device_id) FROM session_devices d WHERE d.session_id = s.session_id), '{}'),
  (SELECT coalesce(array_agg(m ORDER BY m), '{}') FROM unnest(s.models) m),
  coalesce(s.started_at, 'epoch'::timestamptz), coalesce(s.ended_at, 'epoch'::timestamptz),
  ag.main_ms, ag.total_ms, ag.est, us.cost, us.tok,
  (SELECT count(*) FROM failures f WHERE f.session_id = s.session_id),
  coalesce((SELECT array_agg(DISTINCT k.skill ORDER BY k.skill) FROM skill_phases k WHERE k.session_id = s.session_id), '{}'),
  s.line_count, coalesce(s.git_branch, ''), ag.turns
 FROM sessions s LEFT JOIN projects p ON p.path = s.project_path
 CROSS JOIN LATERAL (SELECT coalesce(sum(a.duration_ms) FILTER (WHERE a.agent_id = 'main'), 0)::bigint main_ms,
     coalesce(sum(a.duration_ms), 0)::bigint total_ms, coalesce(bool_or(a.duration_estimated), false) est,
     coalesce(sum(a.turn_count) FILTER (WHERE a.agent_id = 'main'), 0)::bigint turns
   FROM agents a WHERE a.session_id = s.session_id) ag
 CROSS JOIN LATERAL (SELECT coalesce(sum(u.cost_usd), 0)::float8 cost, coalesce(sum(` + tokExpr + `), 0)::bigint tok
   FROM usage u WHERE u.session_id = s.session_id) us `

func (h *Handler) scanSession(row pgx.Row) (SessionItem, error) {
	var s SessionItem
	err := row.Scan(&s.SessionID, &s.Title, &s.Project, &s.ProjectName, &s.DeviceID, &s.DeviceIDs, &s.Models, &s.StartedAt, &s.EndedAt,
		&s.AgentMs, &s.TotalAgentMs, &s.AgentMsEstimated, &s.CostUSD, &s.Tokens, &s.Failures, &s.Skills, &s.LineCount, &s.GitBranch, &s.Turns)
	s.IsLive = s.EndedAt.After(h.now().Add(-2 * time.Minute))
	return s, err
}

func limitParam(r *http.Request, def, hi int) (int, error) {
	s := r.URL.Query().Get("limit")
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, errInvalid
	}
	return min(n, hi), nil
}

func (h *Handler) sessions(w http.ResponseWriter, r *http.Request) {
	p, _ := webauth.PrincipalFrom(r.Context())
	f, err := h.filter(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	limit, err := limitParam(r, 50, 200)
	var curAt *time.Time
	var curID string
	if c := r.URL.Query().Get("cursor"); err == nil && c != "" {
		b, derr := base64.RawURLEncoding.DecodeString(c)
		us, id, ok := strings.Cut(string(b), "|")
		n, perr := strconv.ParseInt(us, 10, 64)
		if derr != nil || !ok || perr != nil {
			err = errInvalid
		} else {
			t := time.UnixMicro(n).UTC()
			curAt, curID = &t, id
		}
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	page := SessionPage{Items: []SessionItem{}}
	err = h.db.WithUser(r.Context(), p.UserID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), sessionSelect+` WHERE `+setPred+`
		  AND ($7::timestamptz IS NULL OR (s.started_at, s.session_id) < ($7::timestamptz, $8::text))
		  ORDER BY s.started_at DESC, s.session_id DESC LIMIT $9`, append(f.args(), curAt, curID, limit+1)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			s, err := h.scanSession(rows)
			if err != nil {
				return err
			}
			page.Items = append(page.Items, s)
		}
		return rows.Err()
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[limit-1]
		c := base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(last.StartedAt.UnixMicro(), 10) + "|" + last.SessionID))
		page.Next = &c
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

func (h *Handler) sessionDetail(w http.ResponseWriter, r *http.Request) {
	p, _ := webauth.PrincipalFrom(r.Context())
	id := r.PathValue("id")
	det := SessionDetail{Agents: []AgentItem{}, SkillPhases: []PhaseItem{}, Failures: []FailureItem{}}
	err := h.db.WithUser(r.Context(), p.UserID, func(tx pgx.Tx) (err error) {
		ctx := r.Context()
		if det.Session, err = h.scanSession(tx.QueryRow(ctx, sessionSelect+` WHERE s.session_id = $1`, id)); err != nil {
			return err
		}
		if err = collect(ctx, tx, `SELECT a.agent_id, coalesce(a.agent_type, ''), coalesce(a.description, ''), a.parent_agent_id, a.spawn_tool_use_id, a.spawn_depth,
		    a.started_at, a.ended_at, a.finished, a.duration_ms, a.duration_estimated,
		    (SELECT coalesce(jsonb_object_agg(x.tool_name, x.n), '{}') FROM (SELECT t.tool_name, count(*) n FROM tool_calls t
		       WHERE t.session_id = a.session_id AND t.agent_id = a.agent_id GROUP BY 1) x),
		    (SELECT count(*) FROM tool_calls t WHERE t.session_id = a.session_id AND t.agent_id = a.agent_id AND t.is_error),
		    us.cost, us.tok, us.inp, us.outp, us.cr, us.cw, us.n,
		    (SELECT t.started_at FROM tool_calls t WHERE t.session_id = a.session_id AND t.tool_use_id = a.spawn_tool_use_id),
		    (SELECT t.ended_at FROM tool_calls t WHERE t.session_id = a.session_id AND t.tool_use_id = a.spawn_tool_use_id)
		  FROM agents a CROSS JOIN LATERAL (SELECT coalesce(sum(u.cost_usd), 0)::float8 cost, coalesce(sum(`+tokExpr+`), 0)::bigint tok,
		      coalesce(sum(u.input_tokens), 0)::bigint inp, coalesce(sum(u.output_tokens), 0)::bigint outp,
		      coalesce(sum(u.cache_read_tokens), 0)::bigint cr, coalesce(sum(u.cache_write_5m_tokens + u.cache_write_1h_tokens), 0)::bigint cw, count(*) n
		    FROM usage u WHERE u.session_id = a.session_id AND u.agent_id = a.agent_id) us
		  WHERE a.session_id = $1 ORDER BY a.agent_id <> 'main', a.started_at NULLS LAST, a.agent_id`, []any{id}, func(rows pgx.Rows) error {
			var a AgentItem
			u := &a.Usage
			err := rows.Scan(&a.AgentID, &a.AgentType, &a.Description, &a.ParentAgentID, &a.SpawnToolUseID, &a.SpawnDepth, &a.StartedAt, &a.EndedAt,
				&a.Finished, &a.DurationMs, &a.DurationEstimated, &a.ToolCalls, &a.ToolErrors, &u.CostUSD, &u.Tokens, &u.Input, &u.Output, &u.CacheRead, &u.CacheWrite, &u.Messages, &a.SpawnedAt, &a.MergedAt)
			det.Agents = append(det.Agents, a)
			return err
		}); err != nil {
			return err
		}
		if err = collect(ctx, tx, `SELECT agent_id, seq, skill, started_at, ended_at FROM skill_phases WHERE session_id = $1 ORDER BY agent_id, seq`, []any{id}, func(rows pgx.Rows) error {
			var ph PhaseItem
			err := rows.Scan(&ph.AgentID, &ph.Seq, &ph.Skill, &ph.StartedAt, &ph.EndedAt)
			det.SkillPhases = append(det.SkillPhases, ph)
			return err
		}); err != nil {
			return err
		}
		return collect(ctx, tx, `SELECT f.category, f.detail, f.occurred_at, f.session_id, f.agent_id, f.limit_type, f.resets_at,
		    CASE WHEN f.category = 'usage_limit' AND f.resets_at IS NOT NULL
		         THEN greatest(0, extract(epoch FROM (`+lostEnd+` - f.occurred_at)))::bigint END,
		    `+lineIDExpr+`
		  FROM failures f WHERE f.session_id = $1 ORDER BY f.occurred_at, f.source_file_id, f."offset"`, []any{id}, func(rows pgx.Rows) error {
			var fi FailureItem
			err := rows.Scan(&fi.Category, &fi.Detail, &fi.OccurredAt, &fi.SessionID, &fi.AgentID, &fi.LimitType, &fi.ResetsAt, &fi.TimeLostS, &fi.LineID)
			det.Failures = append(det.Failures, fi)
			return err
		})
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, det)
}

const maxLinePageBytes = 8 << 20

func (h *Handler) sessionLines(w http.ResponseWriter, r *http.Request) {
	p, _ := webauth.PrincipalFrom(r.Context())
	q := r.URL.Query()
	agent := q.Get("agent")
	if agent == "" {
		agent = "main"
	}
	limit, err := limitParam(r, 100, 500)
	var after int64
	if s := q.Get("after"); err == nil && s != "" {
		if after, err = strconv.ParseInt(s, 10, 64); err != nil || after < 0 {
			err = errInvalid
		}
	}
	var before int64
	if s := q.Get("before"); err == nil && s != "" {
		if before, err = strconv.ParseInt(s, 10, 64); err != nil || before < 1 || q.Get("after") != "" {
			err = errInvalid
		}
	}
	if err != nil {
		fail(w, r, errInvalid)
		return
	}
	page := LinePage{Lines: []LineItem{}}
	err = h.db.WithUser(r.Context(), p.UserID, func(tx pgx.Tx) error {
		return h.readLines(r.Context(), tx, r.PathValue("id"), agent, after, before, limit, &page)
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

// linesQuery selects one page of raw lines plus one row; the LIMIT keeps a long transcript from being read whole.
func linesQuery(cond, files, order string) string {
	return `SELECT r.id, r."offset", octet_length(r.line), CASE WHEN octet_length(r.line) <= ` + strconv.Itoa(protocol.MaxReadLine) + ` THEN r.line END FROM raw_lines r WHERE ` + cond + ` AND ` + files + ` ORDER BY ` + order + ` LIMIT $4`
}

// decodeLine is the JSON of one stored line: itself, a size placeholder when too large to load, or base64 when not JSON.
func decodeLine(offset, n int64, raw []byte) json.RawMessage {
	switch {
	case n > protocol.MaxReadLine:
		b, _ := json.Marshal(map[string]any{"too_large": true, "offset": offset, "size": n})
		return b
	case json.Valid(raw):
		return raw
	}
	b, _ := json.Marshal(map[string]string{"invalid_json_b64": base64.StdEncoding.EncodeToString(raw)})
	return b
}

// readLines pages the lines of one agent: ascending after `after`, or with before > 0 the newest lines below `before`, ascending.
func (h *Handler) readLines(ctx context.Context, tx pgx.Tx, session, agent string, after, before int64, limit int, page *LinePage) error {
	var ok bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM derive_cursor WHERE session_id = $1 AND agent_id = $2 AND kind IN ('main', 'subagent'))`,
		session, agent).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return errNotFound
	}
	const files = `r.source_file_id IN (SELECT c.source_file_id FROM derive_cursor c
	    WHERE c.session_id = $1 AND c.agent_id = $2 AND c.kind IN ('main', 'subagent'))`
	order := "r.id"
	cond := "r.id > $3"
	arg := after
	if before > 0 {
		order, cond, arg = "r.id DESC", "r.id < $3", before
	}
	rows, err := tx.Query(ctx, linesQuery(cond, files, order), session, agent, arg, limit+1) // one more row tells whether a next page exists
	if err != nil {
		return err
	}
	defer rows.Close()
	size := 0
	for rows.Next() {
		if len(page.Lines) == limit || (size >= maxLinePageBytes && len(page.Lines) > 0) { // one more row exists
			if before == 0 {
				next := strconv.FormatInt(page.Lines[len(page.Lines)-1].ID, 10)
				page.Next = &next
			}
			break
		}
		var it LineItem
		var raw []byte
		var n int64
		if err := rows.Scan(&it.ID, &it.Offset, &n, &raw); err != nil {
			return err
		}
		it.Line = decodeLine(it.Offset, n, raw)
		size += len(it.Line) // what goes out: base64 of invalid JSON is a third larger
		page.Lines = append(page.Lines, it)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	if before > 0 {
		slices.Reverse(page.Lines)
	}
	if len(page.Lines) == 0 {
		return nil
	}
	var earlier bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM raw_lines r WHERE r.id < $3 AND `+files+`)`, session, agent, page.Lines[0].ID).Scan(&earlier); err != nil {
		return err
	}
	if earlier {
		prev := strconv.FormatInt(page.Lines[0].ID, 10)
		page.Prev = &prev
	}
	return nil
}
