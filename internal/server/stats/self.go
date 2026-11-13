// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package stats

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/usebraid/braid/internal/server/httpx"
	"github.com/usebraid/braid/internal/server/ingest"
)

// SelfToday is today's aggregates; numbers only.
type SelfToday struct {
	From      time.Time `json:"from"`
	To        time.Time `json:"to"`
	TimeZone  string    `json:"time_zone"`
	Sessions  int64     `json:"sessions"`
	AgentMs   int64     `json:"agent_ms"`
	CostUSD   float64   `json:"cost_usd"`
	Tokens    int64     `json:"tokens"`
	Failures  int64     `json:"failures"`
	TimeLostS int64     `json:"time_lost_s"`
}

// SelfSession is the whole-session figures of one session; numbers and times only.
type SelfSession struct {
	SessionID    string     `json:"session_id"`
	AgentMs      int64      `json:"agent_ms"`
	TotalAgentMs int64      `json:"total_agent_ms"`
	CostUSD      float64    `json:"cost_usd"`
	Tokens       int64      `json:"tokens"`
	StartedAt    *time.Time `json:"started_at"`
	EndedAt      *time.Time `json:"ended_at"`
	Failures     int64      `json:"failures"`
}

// SelfLimit is the active usage limit; an enum string and a time only.
type SelfLimit struct {
	Type     string    `json:"type"`
	ResetsAt time.Time `json:"resets_at"`
}

// Self is the response of GET /v1/stats/self.
type Self struct {
	Today   SelfToday    `json:"today"`
	Session *SelfSession `json:"session"`
	Limit   *SelfLimit   `json:"limit"`
}

func (h *Handler) self(w http.ResponseWriter, r *http.Request) {
	dev, _ := ingest.DeviceFrom(r.Context())
	id := r.URL.Query().Get("session")
	if len(id) > 256 {
		fail(w, r, errInvalid)
		return
	}
	var out Self
	err := h.db.WithUser(r.Context(), dev.UserID, func(tx pgx.Tx) error {
		ctx, t := r.Context(), &out.Today
		if err := tx.QueryRow(ctx, `SELECT z, (date_trunc('day', timezone(z, $1::timestamptz))) AT TIME ZONE z,
		    (date_trunc('day', timezone(z, $1::timestamptz)) + interval '1 day') AT TIME ZONE z
		  FROM (SELECT coalesce(time_zone, 'UTC') z FROM users WHERE id = app_user()) x`, h.now()).Scan(&t.TimeZone, &t.From, &t.To); err != nil {
			return err
		}
		f := Filter{From: t.From, To: t.To}
		a := f.args()
		if err := tx.QueryRow(ctx, `WITH ss AS (SELECT s.session_id FROM sessions s WHERE `+setPred+`)
		  SELECT (SELECT count(*) FROM ss),
		    coalesce((SELECT sum(a.duration_ms) FROM agents a WHERE a.agent_id = 'main' AND a.session_id IN (SELECT session_id FROM ss)), 0)::bigint,
		    (SELECT count(*) FROM failures f WHERE f.occurred_at >= $1 AND f.occurred_at < $2 AND f.session_id IN (SELECT session_id FROM ss))`, a...).
			Scan(&t.Sessions, &t.AgentMs, &t.Failures); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT coalesce(sum(u.cost_usd), 0)::float8, coalesce(sum(`+tokExpr+`), 0)::bigint `+usageSet, a...).Scan(&t.CostUSD, &t.Tokens); err != nil {
			return err
		}
		lost, err := lostIntervals(ctx, tx, `f.occurred_at >= $1 AND f.occurred_at < $2 AND f.session_id IN (SELECT s.session_id FROM sessions s WHERE `+setPred+`)`, a...)
		if err != nil {
			return err
		}
		t.TimeLostS = MergeSeconds(lost)
		var l SelfLimit
		switch err := tx.QueryRow(ctx, `SELECT coalesce(limit_type, 'unknown'), resets_at FROM failures
		  WHERE category = 'usage_limit' AND resets_at > $1 ORDER BY occurred_at DESC LIMIT 1`, h.now()).Scan(&l.Type, &l.ResetsAt); {
		case err == nil:
			out.Limit = &l
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		if id == "" {
			return nil
		}
		s := SelfSession{SessionID: id}
		err = tx.QueryRow(ctx, `SELECT s.started_at, s.ended_at,
		    coalesce((SELECT sum(a.duration_ms) FILTER (WHERE a.agent_id = 'main') FROM agents a WHERE a.session_id = s.session_id), 0)::bigint,
		    coalesce((SELECT sum(a.duration_ms) FROM agents a WHERE a.session_id = s.session_id), 0)::bigint,
		    coalesce((SELECT sum(u.cost_usd) FROM usage u WHERE u.session_id = s.session_id), 0)::float8,
		    coalesce((SELECT sum(`+tokExpr+`) FROM usage u WHERE u.session_id = s.session_id), 0)::bigint,
		    (SELECT count(*) FROM failures f WHERE f.session_id = s.session_id)
		  FROM sessions s WHERE s.session_id = $1`, id).Scan(&s.StartedAt, &s.EndedAt, &s.AgentMs, &s.TotalAgentMs, &s.CostUSD, &s.Tokens, &s.Failures)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // unknown session: null
		}
		out.Session = &s
		return err
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
