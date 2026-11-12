// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package stats serves the /api/v1/stats endpoints: aggregates over the derived tables of the signed-in user.
package stats

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/usebraid/braid/internal/server/db"
	"github.com/usebraid/braid/internal/server/httpx"
	"github.com/usebraid/braid/internal/server/ingest"
	"github.com/usebraid/braid/internal/server/webauth"
)

// Handler serves /api/v1/stats.
type Handler struct {
	db  *db.DB
	now func() time.Time
}

// New returns a Handler.
func New(d *db.DB) *Handler { return &Handler{db: d, now: time.Now} }

// Register mounts the web routes behind auth; devLim is used by the device routes.
func (h *Handler) Register(mux *http.ServeMux, auth *webauth.Auth, devLim *httpx.Limiter) {
	mux.Handle("GET /v1/stats/self", ingest.RequireDevice(h.db, "stats:read:self", devLim, http.HandlerFunc(h.self)))
	mux.Handle("GET /api/v1/stats/overview", auth.Require(http.HandlerFunc(h.overview)))
	mux.Handle("GET /api/v1/stats/live", auth.Require(http.HandlerFunc(h.live)))
	mux.Handle("GET /api/v1/stats/filters", auth.Require(http.HandlerFunc(h.filters)))
	mux.Handle("GET /api/v1/stats/failures", auth.Require(http.HandlerFunc(h.failures)))
	mux.Handle("GET /api/v1/stats/search", auth.Require(http.HandlerFunc(h.search)))
	mux.Handle("GET /api/v1/stats/sessions", auth.Require(http.HandlerFunc(h.sessions)))
	mux.Handle("GET /api/v1/stats/sessions/{id}", auth.Require(http.HandlerFunc(h.sessionDetail)))
	mux.Handle("GET /api/v1/stats/sessions/{id}/lines", auth.Require(http.HandlerFunc(h.sessionLines)))
}

// Filter is parsed from the query string.
type Filter struct {
	From, To                      time.Time
	Device, Project, Model, Skill string
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

var (
	errNotFound = errors.New("not found")
	errInvalid  = errors.New("invalid request")
)

func errJSON(w http.ResponseWriter, status int, code string) {
	httpx.WriteJSON(w, status, map[string]string{"error": code})
}

func fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errNotFound), errors.Is(err, pgx.ErrNoRows):
		errJSON(w, http.StatusNotFound, "not_found")
	case errors.Is(err, errInvalid):
		errJSON(w, http.StatusBadRequest, "invalid_request")
	default:
		slog.Error("stats error", "path", r.URL.Path, "err", err)
		errJSON(w, http.StatusInternalServerError, "internal")
	}
}

func (h *Handler) filter(r *http.Request) (Filter, error) {
	q := r.URL.Query()
	f := Filter{To: h.now().UTC(), Device: strings.ToLower(q.Get("device")), Project: q.Get("project"), Model: q.Get("model"), Skill: q.Get("skill")}
	var err error
	if s := q.Get("to"); s != "" {
		if f.To, err = time.Parse(time.RFC3339, s); err != nil {
			return f, errInvalid
		}
	}
	f.From = f.To.AddDate(0, 0, -30)
	if s := q.Get("from"); s != "" {
		if f.From, err = time.Parse(time.RFC3339, s); err != nil {
			return f, errInvalid
		}
	}
	if !f.From.Before(f.To) || f.From.AddDate(5, 0, 0).Before(f.To) || (f.Device != "" && !uuidRe.MatchString(f.Device)) {
		return f, errInvalid
	}
	for _, v := range []string{f.Project, f.Model, f.Skill} {
		if len(v) > 256 {
			return f, errInvalid
		}
	}
	return f, nil
}

// args are $1..$6 of setPred.
func (f Filter) args() []any { return []any{f.From, f.To, f.Device, f.Project, f.Model, f.Skill} }

// setPred selects the session set S over alias s (arguments: Filter.args).
const setPred = `s.ended_at >= $1 AND s.started_at < $2
  AND ($3 = '' OR EXISTS (SELECT 1 FROM session_devices d WHERE d.session_id = s.session_id AND d.device_id::text = $3))
  AND ($4 = '' OR s.project_path = $4) AND ($5 = '' OR $5 = ANY(s.models))
  AND ($6 = '' OR EXISTS (SELECT 1 FROM skill_phases p WHERE p.session_id = s.session_id AND p.skill = $6))`

// lostEnd is the end of the time lost to a usage-limit failure f: the reset, or the next successful assistant message of any session.
const lostEnd = `least(f.resets_at, coalesce((SELECT min(u.at) FROM usage u WHERE u.at > f.occurred_at), f.resets_at))`

// lostIntervals returns the lost intervals of the usage-limit failures matching cond (over alias f).
func lostIntervals(ctx context.Context, tx pgx.Tx, cond string, args ...any) ([]Interval, error) {
	rows, err := tx.Query(ctx, `SELECT f.occurred_at, `+lostEnd+` FROM failures f
	  WHERE f.category = 'usage_limit' AND f.resets_at IS NOT NULL AND `+cond, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Interval
	for rows.Next() {
		var iv Interval
		if err := rows.Scan(&iv.Start, &iv.End); err != nil {
			return nil, err
		}
		out = append(out, iv)
	}
	return out, rows.Err()
}

// Interval is a half-open time span.
type Interval struct{ Start, End time.Time }

// MergeSeconds returns the length of the union of in in whole seconds; intervals with End <= Start are ignored.
func MergeSeconds(in []Interval) int64 {
	var v []Interval
	for _, iv := range in {
		if iv.End.After(iv.Start) {
			v = append(v, iv)
		}
	}
	sort.Slice(v, func(i, j int) bool { return v[i].Start.Before(v[j].Start) })
	var total time.Duration
	var cur Interval
	for i, iv := range v {
		switch {
		case i == 0:
			cur = iv
		case iv.Start.After(cur.End):
			total += cur.End.Sub(cur.Start)
			cur = iv
		case iv.End.After(cur.End):
			cur.End = iv.End
		}
	}
	if len(v) > 0 {
		total += cur.End.Sub(cur.Start)
	}
	return int64(total / time.Second)
}

// Streak counts consecutive days ending today, or yesterday when today has none yet.
func Streak(days []time.Time, today time.Time) int {
	have := map[time.Time]bool{}
	for _, d := range days {
		have[d] = true
	}
	d := today
	if !have[d] {
		d = d.AddDate(0, 0, -1)
	}
	n := 0
	for have[d] {
		n++
		d = d.AddDate(0, 0, -1)
	}
	return n
}
