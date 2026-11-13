// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package stats

import (
	"encoding/base64"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/usebraid/braid/internal/protocol"
	"github.com/usebraid/braid/internal/server/httpx"
	"github.com/usebraid/braid/internal/server/webauth"
)

// TimelineEvent is one start or resume of a session.
type TimelineEvent struct {
	SessionID   string    `json:"session_id"`
	Kind        string    `json:"kind"`
	At          time.Time `json:"at"`
	DeviceID    *string   `json:"device_id"`
	Model       *string   `json:"model"`
	Project     string    `json:"project"`
	ProjectName string    `json:"project_name"`
	Title       string    `json:"title"`
	IsLive      bool      `json:"is_live"`
	Failures    int64     `json:"failures"`
}

// TimelinePage is one page of events, newest first.
type TimelinePage struct {
	Events []TimelineEvent `json:"events"`
	Next   *string         `json:"next"`
}

func (h *Handler) timeline(w http.ResponseWriter, r *http.Request) {
	p, _ := webauth.PrincipalFrom(r.Context())
	f, err := h.filter(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	limit, err := limitParam(r, 50, 200)
	var curAt *time.Time
	var curSession, curKind string
	var curFile, curOff int64
	if c := r.URL.Query().Get("cursor"); err == nil && c != "" {
		b, derr := base64.RawURLEncoding.DecodeString(c)
		parts := strings.SplitN(string(b), "|", 5) // session id last: it may contain "|"
		if derr != nil || len(parts) != 5 {
			err = errInvalid
		} else {
			us, e1 := strconv.ParseInt(parts[0], 10, 64)
			file, e2 := strconv.ParseInt(parts[2], 10, 64)
			off, e3 := strconv.ParseInt(parts[3], 10, 64)
			if e1 != nil || e2 != nil || e3 != nil {
				err = errInvalid
			}
			t := time.UnixMicro(us).UTC()
			curAt, curKind, curFile, curOff, curSession = &t, parts[1], file, off, parts[4]
		}
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	page := TimelinePage{Events: []TimelineEvent{}}
	var keys []string
	err = h.db.WithUser(r.Context(), p.UserID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT e.session_id, e.kind, e.at, e.device_id::text, e.model, coalesce(s.project_path, ''), coalesce(pr.name, ''),
		    coalesce(s.title, ''), coalesce(s.ended_at, 'epoch'::timestamptz), (SELECT count(*) FROM failures x WHERE x.session_id = s.session_id),
		    e.source_file_id, e."offset"
		  FROM session_events e JOIN sessions s ON s.session_id = e.session_id LEFT JOIN projects pr ON pr.path = s.project_path
		  WHERE e.at >= $1 AND e.at < $2 AND ($3 = '' OR e.device_id::text = $3) AND ($4 = '' OR s.project_path = $4) AND ($5 = '' OR e.model = $5)
		    AND ($6 = '' OR EXISTS (SELECT 1 FROM skill_phases k WHERE k.session_id = s.session_id AND k.skill = $6))
		    AND ($7::timestamptz IS NULL OR (e.at, e.session_id, e.kind, e.source_file_id, e."offset") < ($7::timestamptz, $8::text, $9::text, $10::bigint, $11::bigint))
		  ORDER BY e.at DESC, e.session_id DESC, e.kind DESC, e.source_file_id DESC, e."offset" DESC LIMIT $12`,
			append(f.args(), curAt, curSession, curKind, curFile, curOff, limit+1)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var ev TimelineEvent
			var ended time.Time
			var file, off int64
			if err := rows.Scan(&ev.SessionID, &ev.Kind, &ev.At, &ev.DeviceID, &ev.Model, &ev.Project, &ev.ProjectName, &ev.Title, &ended, &ev.Failures, &file, &off); err != nil {
				return err
			}
			ev.IsLive = ended.After(h.now().Add(-2 * time.Minute))
			page.Events = append(page.Events, ev)
			keys = append(keys, strconv.FormatInt(ev.At.UnixMicro(), 10)+"|"+ev.Kind+"|"+strconv.FormatInt(file, 10)+"|"+strconv.FormatInt(off, 10)+"|"+ev.SessionID)
		}
		return rows.Err()
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	if len(page.Events) > limit {
		page.Events = page.Events[:limit]
		c := base64.RawURLEncoding.EncodeToString([]byte(keys[limit-1]))
		page.Next = &c
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

// ChatLine is one line of any agent of a session; Ts is null while its file has no timestamp yet.
type ChatLine struct {
	LineItem
	AgentID string     `json:"agent_id"`
	Ts      *time.Time `json:"ts"`
}

// ChatPage is one page of the session's lines in time order; Next and Prev are cursors for after and before.
type ChatPage struct {
	Lines []ChatLine `json:"lines"`
	Next  *string    `json:"next"`
	Prev  *string    `json:"prev"`
}

// tsText is lo.ts as UTC text with microseconds, or '-infinity'; the cursor form of a line's time.
const tsText = `CASE WHEN isfinite(lo.ts) THEN to_char(lo.ts AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') ELSE '-infinity' END`

// chatCursor is "<ts as text>|<line id>"; the text form round-trips -infinity.
func chatCursor(ts string, id int64) *string {
	c := base64.RawURLEncoding.EncodeToString([]byte(ts + "|" + strconv.FormatInt(id, 10)))
	return &c
}

func parseChatCursor(c string) (ts string, id int64, err error) {
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return "", 0, errInvalid
	}
	ts, ids, ok := strings.Cut(string(b), "|")
	if id, err = strconv.ParseInt(ids, 10, 64); !ok || err != nil || id < 1 {
		return "", 0, errInvalid
	}
	if _, err := time.Parse(time.RFC3339Nano, ts); err != nil && ts != "-infinity" {
		return "", 0, errInvalid
	}
	return ts, id, nil
}

func (h *Handler) sessionChat(w http.ResponseWriter, r *http.Request) {
	p, _ := webauth.PrincipalFrom(r.Context())
	q := r.URL.Query()
	limit, err := limitParam(r, 200, 500)
	n := 0
	for _, k := range []string{"after", "before", "at"} {
		if q.Get(k) != "" {
			n++
		}
	}
	var curTs string
	var curID int64
	mode := ""
	for _, k := range []string{"after", "before"} {
		if c := q.Get(k); err == nil && c != "" {
			mode = k
			curTs, curID, err = parseChatCursor(c)
		}
	}
	if s := q.Get("at"); err == nil && s != "" {
		mode = "at"
		if curID, err = strconv.ParseInt(s, 10, 64); err != nil || curID < 1 {
			err = errInvalid
		}
	}
	if err != nil || n > 1 {
		fail(w, r, errInvalid)
		return
	}
	page := ChatPage{Lines: []ChatLine{}}
	err = h.db.WithUser(r.Context(), p.UserID, func(tx pgx.Tx) error {
		ctx := r.Context()
		session := r.PathValue("id")
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sessions WHERE session_id = $1)`, session).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return errNotFound
		}
		if mode == "at" { // start at that line: its time is the cursor, inclusive
			if err := tx.QueryRow(ctx, `SELECT `+tsText+` FROM line_order lo WHERE session_id = $1 AND raw_line_id = $2`, session, curID).Scan(&curTs); err != nil {
				return err
			}
		}
		cond, order := "$2::timestamptz IS NULL AND $3::bigint IS NULL", "lo.ts, lo.raw_line_id"
		switch mode {
		case "after":
			cond = "(lo.ts, lo.raw_line_id) > ($2::timestamptz, $3::bigint)"
		case "before":
			cond, order = "(lo.ts, lo.raw_line_id) < ($2::timestamptz, $3::bigint)", "lo.ts DESC, lo.raw_line_id DESC"
		case "at":
			cond = "(lo.ts, lo.raw_line_id) >= ($2::timestamptz, $3::bigint)"
		}
		args := []any{session, nil, nil, limit + 1}
		if mode != "" {
			args[1], args[2] = curTs, curID
		}
		rows, err := tx.Query(ctx, `SELECT r.id, lo.agent_id, `+tsText+`, lo.ts, r."offset", octet_length(r.line),
		    CASE WHEN octet_length(r.line) <= `+strconv.Itoa(protocol.MaxReadLine)+` THEN r.line END
		  FROM line_order lo JOIN raw_lines r ON r.id = lo.raw_line_id
		  WHERE lo.session_id = $1 AND `+cond+` ORDER BY `+order+` LIMIT $4`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		var tss []string // cursor text of each line
		size, more := 0, false
		for rows.Next() {
			if len(page.Lines) == limit || (size >= maxLinePageBytes && len(page.Lines) > 0) {
				more = true
				break
			}
			var cl ChatLine
			var ts pgtype.Timestamptz
			var txt string
			var raw []byte
			var sz int64
			if err := rows.Scan(&cl.ID, &cl.AgentID, &txt, &ts, &cl.Offset, &sz, &raw); err != nil {
				return err
			}
			if ts.Valid && ts.InfinityModifier == pgtype.Finite {
				cl.Ts = &ts.Time
			}
			cl.Line = decodeLine(cl.Offset, sz, raw)
			size += len(cl.Line)
			page.Lines = append(page.Lines, cl)
			tss = append(tss, txt)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows.Close()
		if mode == "before" {
			slices.Reverse(page.Lines)
			slices.Reverse(tss)
		}
		if len(page.Lines) == 0 {
			return nil
		}
		last := len(page.Lines) - 1
		if mode == "before" || more {
			page.Next = chatCursor(tss[last], page.Lines[last].ID)
		}
		var earlier bool
		if mode == "before" && more {
			earlier = true
		} else if mode != "before" {
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM line_order lo WHERE lo.session_id = $1 AND (lo.ts, lo.raw_line_id) < ($2::timestamptz, $3::bigint))`,
				session, tss[0], page.Lines[0].ID).Scan(&earlier); err != nil {
				return err
			}
		}
		if earlier {
			page.Prev = chatCursor(tss[0], page.Lines[0].ID)
		}
		return nil
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}
