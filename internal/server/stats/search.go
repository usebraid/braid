// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package stats

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/usebraid/braid/internal/protocol"
	"github.com/usebraid/braid/internal/server/derive"
	"github.com/usebraid/braid/internal/server/httpx"
	"github.com/usebraid/braid/internal/server/webauth"
)

// SearchHit is one matching prompt or reply; Snippet is cut from the raw line at query time.
type SearchHit struct {
	SessionID string     `json:"session_id"`
	AgentID   string     `json:"agent_id"`
	Role      string     `json:"role"`
	At        *time.Time `json:"at"`
	Title     string     `json:"title"`
	Snippet   string     `json:"snippet"`
	Rank      float64    `json:"rank"`
	LineID    int64      `json:"line_id"` // raw_lines.id of the hit line (highest id at its offset)
}

// SearchResult is the response of GET /api/v1/stats/search.
type SearchResult struct {
	Hits []SearchHit `json:"hits"`
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	p, _ := webauth.PrincipalFrom(r.Context())
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	f, err := h.filter(r)
	var limit int
	if err == nil {
		limit, err = limitParam(r, 20, 50)
	}
	if n := utf8.RuneCountInString(q); err != nil || n < 2 || n > 200 {
		fail(w, r, errInvalid)
		return
	}
	// unlike the other endpoints the range is unbounded unless given (a lone far-past "from" still hits the 5-year cap of filter)
	if r.URL.Query().Get("from") == "" {
		f.From = time.Unix(0, 0).UTC()
	}
	if r.URL.Query().Get("to") == "" {
		f.To = time.Date(9000, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	res := SearchResult{Hits: []SearchHit{}}
	err = h.db.WithUser(r.Context(), p.UserID, func(tx pgx.Tx) error {
		type ref struct{ file, off int64 }
		var refs []ref
		err := collect(r.Context(), tx, `WITH tq AS (SELECT websearch_to_tsquery('simple', $7) AS q)
		  SELECT d.session_id, d.agent_id, d.role, d.at, coalesce(t.title, ''), d.source_file_id, d."offset",
		    (ts_rank(d.tsv, tq.q) + CASE WHEN d.tsv @@ tq.q THEN 0 ELSE 0.01 END)::float8
		  FROM search_docs d JOIN sessions t ON t.session_id = d.session_id, tq
		  WHERE d.session_id IN (SELECT s.session_id FROM sessions s WHERE `+setPred+`)
		    AND (d.tsv @@ tq.q OR d.idents ILIKE '%' || $8 || '%')
		  ORDER BY 8 DESC, d.at DESC NULLS LAST LIMIT $9`, append(f.args(), q, likeEscaper.Replace(q), limit), func(rows pgx.Rows) error {
			var hit SearchHit
			var rf ref
			err := rows.Scan(&hit.SessionID, &hit.AgentID, &hit.Role, &hit.At, &hit.Title, &rf.file, &rf.off, &hit.Rank)
			res.Hits = append(res.Hits, hit)
			refs = append(refs, rf)
			return err
		})
		if err != nil {
			return err
		}
		for i, rf := range refs {
			var raw []byte
			// Lines over MaxReadLine are never loaded: the hit keeps its line id and gets no snippet.
			err := tx.QueryRow(r.Context(), `SELECT id, CASE WHEN octet_length(line) <= $3 THEN line END FROM raw_lines
			  WHERE source_file_id = $1 AND "offset" = $2 ORDER BY id DESC LIMIT 1`, rf.file, rf.off, protocol.MaxReadLine).Scan(&res.Hits[i].LineID, &raw)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			if raw != nil {
				res.Hits[i].Snippet = derive.Snippet(raw, res.Hits[i].Role, q)
			}
		}
		return nil
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}
