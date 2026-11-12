// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package stats

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/usebraid/braid/internal/server/httpx"
	"github.com/usebraid/braid/internal/server/webauth"
)

// CurrentTool is the newest unfinished tool call of an agent.
type CurrentTool struct {
	Name      string    `json:"name"`
	Summary   string    `json:"summary"` // tool_calls.input_summary, <= 200 chars
	StartedAt time.Time `json:"started_at"`
}

// LiveAgent is an agent active within the last two minutes.
type LiveAgent struct {
	AgentID       string       `json:"agent_id"`
	AgentType     string       `json:"agent_type"`
	Description   string       `json:"description"`
	ParentAgentID *string      `json:"parent_agent_id"`
	StartedAt     *time.Time   `json:"started_at"`
	CurrentTool   *CurrentTool `json:"current_tool"` // newest tool call of the agent without ended_at, started < 15 min ago
}

// LiveSession is a live session with its active agents, main first.
type LiveSession struct {
	Session SessionItem `json:"session"`
	Agents  []LiveAgent `json:"agents"`
}

// LivePage is the response of GET /api/v1/stats/live.
type LivePage struct {
	Items []LiveSession `json:"items"`
}

// ProjectOption is one project in the filter dropdown.
type ProjectOption struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// FilterOptions is the response of GET /api/v1/stats/filters.
type FilterOptions struct {
	Projects []ProjectOption `json:"projects"`
	Models   []string        `json:"models"`
	Skills   []string        `json:"skills"`
}

func (h *Handler) live(w http.ResponseWriter, r *http.Request) {
	p, _ := webauth.PrincipalFrom(r.Context())
	now := h.now()
	page := LivePage{Items: []LiveSession{}}
	err := h.db.WithUser(r.Context(), p.UserID, func(tx pgx.Tx) error {
		ctx := r.Context()
		idx := map[string]int{}
		if err := collect(ctx, tx, sessionSelect+` WHERE s.ended_at > $1 ORDER BY s.ended_at DESC LIMIT 50`,
			[]any{now.Add(-2 * time.Minute)}, func(rows pgx.Rows) error {
				s, err := h.scanSession(rows)
				idx[s.SessionID] = len(page.Items)
				page.Items = append(page.Items, LiveSession{Session: s, Agents: []LiveAgent{}})
				return err
			}); err != nil || len(page.Items) == 0 {
			return err
		}
		ids := make([]string, 0, len(idx))
		for id := range idx {
			ids = append(ids, id)
		}
		at := map[[2]string]*LiveAgent{}
		if err := collect(ctx, tx, `SELECT a.session_id, a.agent_id, coalesce(a.agent_type, ''), coalesce(a.description, ''), a.parent_agent_id, a.started_at
		  FROM agents a WHERE a.session_id = ANY($1) AND a.ended_at > $2 AND a.finished IS NOT TRUE
		  ORDER BY a.session_id, a.agent_id <> 'main', a.started_at NULLS LAST, a.agent_id`, []any{ids, now.Add(-2 * time.Minute)}, func(rows pgx.Rows) error {
			var sid string
			var a LiveAgent
			if err := rows.Scan(&sid, &a.AgentID, &a.AgentType, &a.Description, &a.ParentAgentID, &a.StartedAt); err != nil {
				return err
			}
			it := &page.Items[idx[sid]]
			it.Agents = append(it.Agents, a)
			at[[2]string{sid, a.AgentID}] = &it.Agents[len(it.Agents)-1]
			return nil
		}); err != nil {
			return err
		}
		// Pointers into Agents stay valid: no appends after this point.
		return collect(ctx, tx, `SELECT DISTINCT ON (t.session_id, t.agent_id) t.session_id, t.agent_id, t.tool_name, t.input_summary, t.started_at
		  FROM tool_calls t WHERE t.session_id = ANY($1) AND t.ended_at IS NULL AND t.started_at > $2
		  ORDER BY t.session_id, t.agent_id, t.started_at DESC`, []any{ids, now.Add(-15 * time.Minute)}, func(rows pgx.Rows) error {
			var sid, aid string
			var c CurrentTool
			if err := rows.Scan(&sid, &aid, &c.Name, &c.Summary, &c.StartedAt); err != nil {
				return err
			}
			if a := at[[2]string{sid, aid}]; a != nil {
				a.CurrentTool = &c
			}
			return nil
		})
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

func (h *Handler) filters(w http.ResponseWriter, r *http.Request) {
	p, _ := webauth.PrincipalFrom(r.Context())
	f, err := h.filter(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	f.Device, f.Project, f.Model, f.Skill = "", "", "", "" // options are not narrowed by the current selection
	out := FilterOptions{Projects: []ProjectOption{}, Models: []string{}, Skills: []string{}}
	err = h.db.WithUser(r.Context(), p.UserID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := collect(ctx, tx, `SELECT DISTINCT s.project_path, coalesce(p.name, '') FROM sessions s LEFT JOIN projects p ON p.path = s.project_path
		  WHERE s.project_path IS NOT NULL AND `+setPred+` ORDER BY 1`, f.args(), func(rows pgx.Rows) error {
			var o ProjectOption
			err := rows.Scan(&o.Path, &o.Name)
			out.Projects = append(out.Projects, o)
			return err
		}); err != nil {
			return err
		}
		if err := collect(ctx, tx, `SELECT DISTINCT m FROM sessions s, unnest(s.models) m WHERE `+setPred+` ORDER BY 1`, f.args(), func(rows pgx.Rows) error {
			var m string
			err := rows.Scan(&m)
			out.Models = append(out.Models, m)
			return err
		}); err != nil {
			return err
		}
		return collect(ctx, tx, `SELECT DISTINCT k.skill FROM skill_phases k JOIN sessions s ON s.session_id = k.session_id WHERE `+setPred+` ORDER BY 1`, f.args(), func(rows pgx.Rows) error {
			var sk string
			err := rows.Scan(&sk)
			out.Skills = append(out.Skills, sk)
			return err
		})
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
