// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package stats

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/usebraid/braid/internal/server/httpx"
	"github.com/usebraid/braid/internal/server/webauth"
)

// KPI is the headline figures of the selected sessions.
type KPI struct {
	Sessions          int64    `json:"sessions"`
	AgentMs           int64    `json:"agent_ms"`
	TotalAgentMs      int64    `json:"total_agent_ms"`
	AgentMsEstimated  bool     `json:"agent_ms_estimated"`
	CostUSD           float64  `json:"cost_usd"`
	CostUnknownModels []string `json:"cost_unknown_models"`
	Tokens            int64    `json:"tokens"`
	StreakDays        int      `json:"streak_days"`
	Failures          int64    `json:"failures"`
	TimeLostS         int64    `json:"time_lost_s"`
}

// DayPoint is one local day.
type DayPoint struct {
	Day      string  `json:"day"`
	CostUSD  float64 `json:"cost_usd"`
	Tokens   int64   `json:"tokens"`
	Sessions int64   `json:"sessions"`
}

// ModelPoint is one model.
type ModelPoint struct {
	Model        string  `json:"model"`
	CostUSD      float64 `json:"cost_usd"`
	Tokens       int64   `json:"tokens"`
	Messages     int64   `json:"messages"`
	PriceUnknown bool    `json:"price_unknown"`
}

// ProjectPoint is one project.
type ProjectPoint struct {
	Path     string  `json:"path"`
	Name     string  `json:"name"`
	CostUSD  float64 `json:"cost_usd"`
	Tokens   int64   `json:"tokens"`
	Sessions int64   `json:"sessions"`
	AgentMs  int64   `json:"agent_ms"`
}

// HeatCell is the number of assistant messages in one weekday/hour slot (weekday 0 = Sunday, user time zone).
type HeatCell struct {
	Weekday int   `json:"weekday"`
	Hour    int   `json:"hour"`
	Count   int64 `json:"count"`
}

// Overview is the response of GET /api/v1/stats/overview.
type Overview struct {
	From       time.Time      `json:"from"`
	To         time.Time      `json:"to"`
	TimeZone   string         `json:"time_zone"`
	Reindexing bool           `json:"reindexing"`
	KPI        KPI            `json:"kpi"`
	PerDay     []DayPoint     `json:"per_day"`
	PerModel   []ModelPoint   `json:"per_model"`
	PerProject []ProjectPoint `json:"per_project"`
	Heatmap    []HeatCell     `json:"heatmap"`
}

const (
	tokExpr = `(u.input_tokens + u.output_tokens + u.cache_read_tokens + u.cache_write_5m_tokens + u.cache_write_1h_tokens)`
	// usageSet: usage rows of S within the range ($1, $2 and the rest as in setPred).
	usageSet = `FROM usage u WHERE u.at >= $1 AND u.at < $2 AND u.session_id IN (SELECT s.session_id FROM sessions s WHERE ` + setPred + `)`
)

func (h *Handler) overview(w http.ResponseWriter, r *http.Request) {
	p, _ := webauth.PrincipalFrom(r.Context())
	f, err := h.filter(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	ov := Overview{From: f.From, To: f.To, PerDay: []DayPoint{}, PerModel: []ModelPoint{}, PerProject: []ProjectPoint{}, Heatmap: []HeatCell{}}
	ov.KPI.CostUnknownModels = []string{}
	err = h.db.WithUser(r.Context(), p.UserID, func(tx pgx.Tx) error { return h.fillOverview(r.Context(), tx, f, &ov) })
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ov)
}

func (h *Handler) fillOverview(ctx context.Context, tx pgx.Tx, f Filter, ov *Overview) error {
	a := f.args()
	if err := tx.QueryRow(ctx, `SELECT coalesce(time_zone, 'UTC'), coalesce((SELECT rebuilding FROM derive_state WHERE user_id = app_user()), false)
	  FROM users WHERE id = app_user()`).Scan(&ov.TimeZone, &ov.Reindexing); err != nil {
		return err
	}
	tz := append(append([]any{}, a...), ov.TimeZone) // $7
	k := &ov.KPI
	if err := tx.QueryRow(ctx, `WITH ss AS (SELECT s.session_id FROM sessions s WHERE `+setPred+`)
	  SELECT (SELECT count(*) FROM ss),
	    coalesce((SELECT sum(a.duration_ms) FROM agents a WHERE a.agent_id = 'main' AND a.session_id IN (SELECT session_id FROM ss)), 0)::bigint,
	    coalesce((SELECT sum(a.duration_ms) FROM agents a WHERE a.session_id IN (SELECT session_id FROM ss)), 0)::bigint,
	    coalesce((SELECT bool_or(a.duration_estimated) FROM agents a WHERE a.session_id IN (SELECT session_id FROM ss)), false),
	    (SELECT count(*) FROM failures f WHERE f.occurred_at >= $1 AND f.occurred_at < $2 AND f.session_id IN (SELECT session_id FROM ss))`, a...).
		Scan(&k.Sessions, &k.AgentMs, &k.TotalAgentMs, &k.AgentMsEstimated, &k.Failures); err != nil {
		return err
	}
	var unknown []string
	if err := tx.QueryRow(ctx, `SELECT coalesce(sum(u.cost_usd), 0)::float8, coalesce(sum(`+tokExpr+`), 0)::bigint,
	  coalesce(array_agg(DISTINCT u.model ORDER BY u.model) FILTER (WHERE u.price_unknown), '{}') `+usageSet, a...).Scan(&k.CostUSD, &k.Tokens, &unknown); err != nil {
		return err
	}
	if unknown != nil {
		k.CostUnknownModels = unknown
	}
	lost, err := lostIntervals(ctx, tx, `f.occurred_at >= $1 AND f.occurred_at < $2 AND f.session_id IN (SELECT s.session_id FROM sessions s WHERE `+setPred+`)`, a...)
	if err != nil {
		return err
	}
	k.TimeLostS = MergeSeconds(lost)
	if k.StreakDays, err = h.streak(ctx, tx, ov.TimeZone); err != nil {
		return err
	}

	if err := collect(ctx, tx, `WITH d1 AS (SELECT date(timezone($7, u.at)) AS day, sum(u.cost_usd)::float8 cost, sum(`+tokExpr+`)::bigint tok `+usageSet+` GROUP BY 1),
	  d2 AS (SELECT date(timezone($7, s.started_at)) AS day, count(*) n FROM sessions s WHERE `+setPred+` GROUP BY 1)
	  SELECT to_char(coalesce(d1.day, d2.day), 'YYYY-MM-DD'), coalesce(cost, 0), coalesce(tok, 0), coalesce(n, 0)
	    FROM d1 FULL JOIN d2 ON d1.day = d2.day ORDER BY 1`, tz, func(rows pgx.Rows) error {
		var d DayPoint
		err := rows.Scan(&d.Day, &d.CostUSD, &d.Tokens, &d.Sessions)
		ov.PerDay = append(ov.PerDay, d)
		return err
	}); err != nil {
		return err
	}
	if err := collect(ctx, tx, `SELECT u.model, sum(u.cost_usd)::float8, sum(`+tokExpr+`)::bigint, count(*), bool_or(u.price_unknown) `+usageSet+`
	  GROUP BY u.model ORDER BY 2 DESC, 1`, a, func(rows pgx.Rows) error {
		var m ModelPoint
		err := rows.Scan(&m.Model, &m.CostUSD, &m.Tokens, &m.Messages, &m.PriceUnknown)
		ov.PerModel = append(ov.PerModel, m)
		return err
	}); err != nil {
		return err
	}
	if err := collect(ctx, tx, `SELECT coalesce(s.project_path, ''), coalesce(max(p.name), ''), coalesce(sum(c.cost), 0)::float8, coalesce(sum(c.tok), 0)::bigint,
	    count(*), coalesce(sum(am.ms), 0)::bigint
	  FROM sessions s LEFT JOIN projects p ON p.path = s.project_path
	  LEFT JOIN LATERAL (SELECT sum(u.cost_usd) cost, sum(`+tokExpr+`) tok FROM usage u
	                      WHERE u.session_id = s.session_id AND u.at >= $1 AND u.at < $2) c ON true
	  LEFT JOIN LATERAL (SELECT a.duration_ms ms FROM agents a WHERE a.session_id = s.session_id AND a.agent_id = 'main') am ON true
	  WHERE `+setPred+` GROUP BY 1 ORDER BY 3 DESC, 1`, a, func(rows pgx.Rows) error {
		var pp ProjectPoint
		err := rows.Scan(&pp.Path, &pp.Name, &pp.CostUSD, &pp.Tokens, &pp.Sessions, &pp.AgentMs)
		ov.PerProject = append(ov.PerProject, pp)
		return err
	}); err != nil {
		return err
	}
	return collect(ctx, tx, `SELECT extract(dow FROM timezone($7, u.at))::int, extract(hour FROM timezone($7, u.at))::int, count(*) `+usageSet+`
	  GROUP BY 1, 2 ORDER BY 1, 2`, tz, func(rows pgx.Rows) error {
		var c HeatCell
		err := rows.Scan(&c.Weekday, &c.Hour, &c.Count)
		ov.Heatmap = append(ov.Heatmap, c)
		return err
	})
}

// streak ignores the filter: distinct local days with usage over all time.
func (h *Handler) streak(ctx context.Context, tx pgx.Tx, tz string) (int, error) {
	var today time.Time
	if err := tx.QueryRow(ctx, `SELECT date(timezone($1, $2::timestamptz))`, tz, h.now()).Scan(&today); err != nil {
		return 0, err
	}
	var days []time.Time
	err := collect(ctx, tx, `SELECT date(timezone($1, at)) FROM usage GROUP BY 1`, []any{tz}, func(rows pgx.Rows) error {
		var d time.Time
		err := rows.Scan(&d)
		days = append(days, d)
		return err
	})
	return Streak(days, today), err
}

func collect(ctx context.Context, tx pgx.Tx, q string, args []any, each func(pgx.Rows) error) error {
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := each(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}
