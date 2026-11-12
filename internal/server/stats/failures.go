// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package stats

import (
	"encoding/base64"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/usebraid/braid/internal/server/httpx"
	"github.com/usebraid/braid/internal/server/webauth"
)

// FailureAgg is one aggregation bucket; TimeLostS is the union of the lost intervals within the key.
type FailureAgg struct {
	Key       string `json:"key"`
	Count     int64  `json:"count"`
	TimeLostS int64  `json:"time_lost_s"`
}

// FailurePage is the response of GET /api/v1/stats/failures.
type FailurePage struct {
	Items       []FailureItem `json:"items"`
	Next        *string       `json:"next"`
	ByCategory  []FailureAgg  `json:"by_category"`
	ByLimitType []FailureAgg  `json:"by_limit_type"`
	ByDay       []FailureAgg  `json:"by_day"`
	TimeLostS   int64         `json:"time_lost_s"`
}

type failRow struct {
	FailureItem
	file, off int64
	day       string
	iv        *Interval
}

type aggAcc struct {
	n  int64
	iv []Interval
}

func (a *aggAcc) add(r failRow) {
	a.n++
	if r.iv != nil {
		a.iv = append(a.iv, *r.iv)
	}
}

func aggs(m map[string]*aggAcc, byCount bool) []FailureAgg {
	out := make([]FailureAgg, 0, len(m))
	for k, a := range m {
		out = append(out, FailureAgg{Key: k, Count: a.n, TimeLostS: MergeSeconds(a.iv)})
	}
	sort.Slice(out, func(i, j int) bool {
		if byCount && out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func (h *Handler) failures(w http.ResponseWriter, r *http.Request) {
	p, _ := webauth.PrincipalFrom(r.Context())
	f, err := h.filter(r)
	var limit int
	if err == nil {
		limit, err = limitParam(r, 50, 200)
	}
	var curAt time.Time
	var curFile, curOff int64
	hasCur := false
	if c := r.URL.Query().Get("cursor"); err == nil && c != "" {
		b, derr := base64.RawURLEncoding.DecodeString(c)
		parts := strings.Split(string(b), "|")
		if derr != nil || len(parts) != 3 {
			err = errInvalid
		} else {
			us, e1 := strconv.ParseInt(parts[0], 10, 64)
			var e2, e3 error
			curFile, e2 = strconv.ParseInt(parts[1], 10, 64)
			curOff, e3 = strconv.ParseInt(parts[2], 10, 64)
			if e1 != nil || e2 != nil || e3 != nil {
				err = errInvalid
			}
			curAt, hasCur = time.UnixMicro(us).UTC(), true
		}
	}
	if err != nil {
		fail(w, r, errInvalid)
		return
	}
	page := FailurePage{Items: []FailureItem{}, ByCategory: []FailureAgg{}, ByLimitType: []FailureAgg{}, ByDay: []FailureAgg{}}
	err = h.db.WithUser(r.Context(), p.UserID, func(tx pgx.Tx) error {
		var tz string
		if err := tx.QueryRow(r.Context(), `SELECT coalesce(time_zone, 'UTC') FROM users WHERE id = app_user()`).Scan(&tz); err != nil {
			return err
		}
		// shortcut: all matching failures are loaded and aggregated in Go, fine for personal volumes; push into SQL if lists grow past ~100k.
		rows, err := tx.Query(r.Context(), `SELECT f.category, f.detail, f.occurred_at, f.session_id, f.agent_id, f.limit_type, f.resets_at,
		    CASE WHEN f.category = 'usage_limit' AND f.resets_at IS NOT NULL THEN `+lostEnd+` END,
		    f.source_file_id, f."offset", to_char(date(timezone($7, f.occurred_at)), 'YYYY-MM-DD')
		  FROM failures f WHERE f.occurred_at >= $1 AND f.occurred_at < $2
		    AND f.session_id IN (SELECT s.session_id FROM sessions s WHERE `+setPred+`)
		  ORDER BY f.occurred_at DESC, f.source_file_id DESC, f."offset" DESC`, append(f.args(), tz)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		var all []failRow
		for rows.Next() {
			var fr failRow
			var end *time.Time
			if err := rows.Scan(&fr.Category, &fr.Detail, &fr.OccurredAt, &fr.SessionID, &fr.AgentID, &fr.LimitType, &fr.ResetsAt, &end, &fr.file, &fr.off, &fr.day); err != nil {
				return err
			}
			if end != nil {
				fr.iv = &Interval{fr.OccurredAt, *end}
				s := max(0, int64(end.Sub(fr.OccurredAt)/time.Second))
				fr.TimeLostS = &s
			}
			all = append(all, fr)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		cat, lim, day := map[string]*aggAcc{}, map[string]*aggAcc{}, map[string]*aggAcc{}
		var every []Interval
		var kept failRow
		get := func(m map[string]*aggAcc, k string) *aggAcc {
			if m[k] == nil {
				m[k] = &aggAcc{}
			}
			return m[k]
		}
		for _, fr := range all {
			get(cat, fr.Category).add(fr)
			get(day, fr.day).add(fr)
			if fr.Category == "usage_limit" && fr.LimitType != nil {
				get(lim, *fr.LimitType).add(fr)
			}
			if fr.iv != nil {
				every = append(every, *fr.iv)
			}
			after := !hasCur || fr.OccurredAt.Before(curAt) || (fr.OccurredAt.Equal(curAt) && (fr.file < curFile || (fr.file == curFile && fr.off < curOff)))
			if after && len(page.Items) <= limit {
				page.Items = append(page.Items, fr.FailureItem)
				if len(page.Items) == limit+1 { // one more exists: the cursor is the last row kept
					c := base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(kept.OccurredAt.UnixMicro(), 10) + "|" + strconv.FormatInt(kept.file, 10) + "|" + strconv.FormatInt(kept.off, 10)))
					page.Next = &c
				}
				kept = fr
			}
		}
		page.ByCategory, page.ByLimitType, page.ByDay = aggs(cat, true), aggs(lim, true), aggs(day, false)
		page.TimeLostS = MergeSeconds(every)
		return nil
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}
