// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package stats

import (
	"context"
	"net/http"
	"sort"
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
	LongestAgentMs    int64    `json:"longest_agent_ms"`
	Failures          int64    `json:"failures"`
	TimeLostS         int64    `json:"time_lost_s"`
}

// DayPoint is one local day.
type DayPoint struct {
	Day      string  `json:"day"`
	CostUSD  float64 `json:"cost_usd"`
	Tokens   int64   `json:"tokens"`
	Sessions int64   `json:"sessions"`
	AgentMs  int64   `json:"agent_ms"`
}

// Totals are the headline sums of a period; Overview.Previous holds them for the period of the same length before From.
type Totals struct {
	Sessions int64   `json:"sessions"`
	AgentMs  int64   `json:"agent_ms"`
	CostUSD  float64 `json:"cost_usd"`
	Tokens   int64   `json:"tokens"`
}

// ActivityDay is the main-agent time of the sessions started on one local day.
type ActivityDay struct {
	Day     string `json:"day"`
	AgentMs int64  `json:"agent_ms"`
}

// activityDays is the window of Overview.Activity, ending at To: twelve weeks.
const activityDays = 84

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

// ProjectKey names one series of ProjectSplit; the trailing "_other" key collects every project beyond the top eight.
type ProjectKey struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// ProjectValue is one project's share of a day: usage by its time, sessions and agent time by the session's start.
type ProjectValue struct {
	CostUSD  float64 `json:"cost_usd"`
	Tokens   int64   `json:"tokens"`
	Sessions int64   `json:"sessions"`
	AgentMs  int64   `json:"agent_ms"`
}

// ProjectDay is one local day; Values[i] belongs to ProjectSplit.Projects[i].
type ProjectDay struct {
	Day    string         `json:"day"`
	Values []ProjectValue `json:"values"`
}

// ProjectSplit is the daily cost, tokens, sessions and agent time stacked by project.
type ProjectSplit struct {
	Projects []ProjectKey `json:"projects"`
	Days     []ProjectDay `json:"days"`
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
	Previous   Totals         `json:"previous"`
	Activity   []ActivityDay  `json:"activity"`

	PerDayByProject ProjectSplit `json:"per_day_by_project"`
}

const (
	// mainMs joins the main agent's time of session s as am.ms.
	mainMs  = `LEFT JOIN LATERAL (SELECT a.duration_ms ms FROM agents a WHERE a.session_id = s.session_id AND a.agent_id = 'main') am ON true`
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
	ov := Overview{From: f.From, To: f.To, PerDay: []DayPoint{}, PerModel: []ModelPoint{}, PerProject: []ProjectPoint{}, Heatmap: []HeatCell{}, Activity: []ActivityDay{}, PerDayByProject: ProjectSplit{Projects: []ProjectKey{}, Days: []ProjectDay{}}}
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
	    (SELECT count(*) FROM failures f WHERE f.occurred_at >= $1 AND f.occurred_at < $2 AND f.session_id IN (SELECT session_id FROM ss)),
	    coalesce((SELECT max(a.duration_ms) FROM agents a WHERE a.agent_id = 'main' AND a.session_id IN (SELECT session_id FROM ss)), 0)::bigint`, a...).
		Scan(&k.Sessions, &k.AgentMs, &k.TotalAgentMs, &k.AgentMsEstimated, &k.Failures, &k.LongestAgentMs); err != nil {
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
	prev := f
	prev.From, prev.To = f.From.Add(-f.To.Sub(f.From)), f.From
	if ov.Previous, err = periodTotals(ctx, tx, prev); err != nil {
		return err
	}
	act := f
	act.From = f.To.AddDate(0, 0, -activityDays)
	if err := collect(ctx, tx, `SELECT to_char(date(timezone($7, s.started_at)), 'YYYY-MM-DD'), coalesce(sum(am.ms), 0)::bigint
	  FROM sessions s `+mainMs+` WHERE `+setPred+` GROUP BY 1 ORDER BY 1`, append(act.args(), ov.TimeZone), func(rows pgx.Rows) error {
		var d ActivityDay
		err := rows.Scan(&d.Day, &d.AgentMs)
		ov.Activity = append(ov.Activity, d)
		return err
	}); err != nil {
		return err
	}

	if err := collect(ctx, tx, `WITH d1 AS (SELECT date(timezone($7, u.at)) AS day, sum(u.cost_usd)::float8 cost, sum(`+tokExpr+`)::bigint tok `+usageSet+` GROUP BY 1),
	  d2 AS (SELECT date(timezone($7, s.started_at)) AS day, count(*) n, coalesce(sum(am.ms), 0) ms FROM sessions s `+mainMs+` WHERE `+setPred+` GROUP BY 1)
	  SELECT to_char(coalesce(d1.day, d2.day), 'YYYY-MM-DD'), coalesce(cost, 0), coalesce(tok, 0), coalesce(n, 0), coalesce(ms, 0)::bigint
	    FROM d1 FULL JOIN d2 ON d1.day = d2.day ORDER BY 1`, tz, func(rows pgx.Rows) error {
		var d DayPoint
		err := rows.Scan(&d.Day, &d.CostUSD, &d.Tokens, &d.Sessions, &d.AgentMs)
		ov.PerDay = append(ov.PerDay, d)
		return err
	}); err != nil {
		return err
	}
	if err := fillProjectSplit(ctx, tx, tz, ov); err != nil {
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
	  `+mainMs+`
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

// maxSplitProjects is the number of projects kept before the rest is folded into "_other".
const maxSplitProjects = 8

// fillProjectSplit fills ov.PerDayByProject over the days of ov.PerDay (so both always align); tz is the argument list with the time zone as $7.
func fillProjectSplit(ctx context.Context, tx pgx.Tx, tz []any, ov *Overview) error {
	type cell struct {
		day, key, name string
		v              ProjectValue
	}
	var cells []cell
	total := map[string]float64{}
	names := map[string]string{}
	if err := collect(ctx, tx, `SELECT to_char(date(timezone($7, u.at)), 'YYYY-MM-DD'), coalesce(s.project_path, ''), coalesce(max(p.name), ''),
	    sum(u.cost_usd)::float8, sum(`+tokExpr+`)::bigint
	  FROM usage u JOIN sessions s ON s.session_id = u.session_id LEFT JOIN projects p ON p.path = s.project_path
	  WHERE u.at >= $1 AND u.at < $2 AND u.session_id IN (SELECT s.session_id FROM sessions s WHERE `+setPred+`)
	  GROUP BY 1, 2`, tz, func(rows pgx.Rows) error {
		var c cell
		err := rows.Scan(&c.day, &c.key, &c.name, &c.v.CostUSD, &c.v.Tokens)
		cells = append(cells, c)
		total[c.key] += c.v.CostUSD
		names[c.key] = c.name
		return err
	}); err != nil {
		return err
	}
	// sessions and agent time count on the day the session started; a project may have them without usage in range
	if err := collect(ctx, tx, `SELECT to_char(date(timezone($7, s.started_at)), 'YYYY-MM-DD'), coalesce(s.project_path, ''), coalesce(max(p.name), ''),
	    count(*), coalesce(sum(am.ms), 0)::bigint
	  FROM sessions s LEFT JOIN projects p ON p.path = s.project_path `+mainMs+`
	  WHERE `+setPred+` GROUP BY 1, 2`, tz, func(rows pgx.Rows) error {
		var c cell
		err := rows.Scan(&c.day, &c.key, &c.name, &c.v.Sessions, &c.v.AgentMs)
		cells = append(cells, c)
		total[c.key] += 0 // ranks the project even without usage
		names[c.key] = c.name
		return err
	}); err != nil {
		return err
	}
	keys := make([]string, 0, len(total))
	for k := range total {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if total[keys[i]] != total[keys[j]] {
			return total[keys[i]] > total[keys[j]]
		}
		return keys[i] < keys[j]
	})
	idx := map[string]int{}
	sp := &ov.PerDayByProject
	for i, k := range keys {
		if i == maxSplitProjects {
			break
		}
		idx[k] = i
		name := names[k]
		if name == "" {
			name = k
		}
		sp.Projects = append(sp.Projects, ProjectKey{Key: k, Name: name})
	}
	other := -1
	if len(keys) > maxSplitProjects {
		other = len(sp.Projects)
		sp.Projects = append(sp.Projects, ProjectKey{Key: "_other", Name: "Other"})
	}
	byDay := map[string][]ProjectValue{}
	for _, d := range ov.PerDay {
		byDay[d.Day] = make([]ProjectValue, len(sp.Projects))
	}
	for _, c := range cells {
		i, ok := idx[c.key]
		if !ok {
			i = other
		}
		v := &byDay[c.day][i]
		v.CostUSD += c.v.CostUSD
		v.Tokens += c.v.Tokens
		v.Sessions += c.v.Sessions
		v.AgentMs += c.v.AgentMs
	}
	for _, d := range ov.PerDay {
		sp.Days = append(sp.Days, ProjectDay{Day: d.Day, Values: byDay[d.Day]})
	}
	return nil
}

// periodTotals sums the sessions of f: count and main-agent time by session, cost and tokens by usage time (as KPI).
func periodTotals(ctx context.Context, tx pgx.Tx, f Filter) (Totals, error) {
	var t Totals
	a := f.args()
	if err := tx.QueryRow(ctx, `SELECT count(*), coalesce(sum(am.ms), 0)::bigint FROM sessions s `+mainMs+` WHERE `+setPred, a...).
		Scan(&t.Sessions, &t.AgentMs); err != nil {
		return t, err
	}
	err := tx.QueryRow(ctx, `SELECT coalesce(sum(u.cost_usd), 0)::float8, coalesce(sum(`+tokExpr+`), 0)::bigint `+usageSet, a...).Scan(&t.CostUSD, &t.Tokens)
	return t, err
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
