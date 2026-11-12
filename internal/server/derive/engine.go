// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package derive turns raw transcript rows into the derived tables, exactly once per row.
package derive

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/usebraid/braid/internal/server/db"
	"github.com/usebraid/braid/internal/server/derive/parse"
)

// ParserVersion is bumped whenever a parser or engine change requires re-deriving from raw data.
const ParserVersion = 1

// Deriver derives the pending raw rows of users.
type Deriver struct {
	DB         *db.DB
	BatchLines int // raw rows per transaction, default 5000
	BatchBytes int // a chunk stops once this many raw bytes were read (at least one row), default 32 MiB
}

type pendingFile struct {
	id       int64
	rel      string
	snapshot bool
}

// DeriveUser derives everything pending for userID and returns the number of raw rows (lines + snapshots) processed.
func (d *Deriver) DeriveUser(ctx context.Context, userID string) (rows int, err error) {
	var files []pendingFile
	err = d.DB.WithUser(ctx, userID, func(tx pgx.Tx) error {
		r, err := tx.Query(ctx, `
		  SELECT f.id, f.rel_path, f.kind = 'snapshot'
		    FROM source_files f LEFT JOIN derive_cursor c ON c.source_file_id = f.id AND c.user_id = f.user_id
		   WHERE f.user_id = $1
		     AND ((f.kind = 'append' AND EXISTS (SELECT 1 FROM raw_lines l WHERE l.source_file_id = f.id AND l.id > coalesce(c.last_line_id, 0)))
		       OR (f.kind = 'snapshot' AND (SELECT max(s.id) FROM raw_snapshots s WHERE s.source_file_id = f.id) > coalesce(c.last_snapshot_id, 0)))
		   ORDER BY f.id`, userID)
		if err != nil {
			return err
		}
		defer r.Close()
		for r.Next() {
			var f pendingFile
			if err := r.Scan(&f.id, &f.rel, &f.snapshot); err != nil {
				return err
			}
			files = append(files, f)
		}
		return r.Err()
	})
	if err != nil {
		return 0, err
	}
	for _, f := range files {
		for {
			n, err := d.chunk(ctx, userID, f)
			if err != nil {
				return rows, fmt.Errorf("derive file %d: %w", f.id, err)
			}
			if n == 0 {
				break
			}
			rows += n
		}
	}
	return rows, nil
}

// Reset deletes every derived row and cursor of the user (never raw data, never derive_state).
func (d *Deriver) Reset(ctx context.Context, userID string) error {
	return d.DB.WithUser(ctx, userID, func(tx pgx.Tx) error {
		for _, t := range []string{"derive_cursor", "derive_errors", "projects", "sessions", "session_devices", "agents",
			"tool_calls", "skill_phases", "usage", "failures", "search_docs"} {
			if _, err := tx.Exec(ctx, `DELETE FROM `+t+` WHERE user_id = $1`, userID); err != nil {
				return err
			}
		}
		return nil
	})
}

type rawRow struct {
	id, offset int64
	device     string
	data       []byte
}

// chunk processes one batch of one file in a single transaction and returns the raw rows consumed.
func (d *Deriver) chunk(ctx context.Context, userID string, f pendingFile) (n int, err error) {
	maxLines, maxBytes := d.BatchLines, d.BatchBytes
	if maxLines <= 0 {
		maxLines = 5000
	}
	if maxBytes <= 0 {
		maxBytes = 32 << 20
	}
	info := parse.ClassifyPath(f.rel)
	kind := map[parse.FileKind]string{parse.FileMain: "main", parse.FileSubagent: "subagent", parse.FileAgentMeta: "agent_meta", parse.FileCustomTitle: "custom_title"}[info.Kind]
	if kind == "" || f.snapshot != (info.Kind == parse.FileAgentMeta || info.Kind == parse.FileCustomTitle) {
		kind, info = "other", parse.FileInfo{}
	}
	err = d.DB.WithUser(ctx, userID, func(tx pgx.Tx) error {
		n = 0
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7002, hashtext($1::text))`, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO derive_cursor(user_id, source_file_id, kind, session_id, agent_id)
		  VALUES ($1, $2, $3, nullif($4, ''), nullif($5, '')) ON CONFLICT DO NOTHING`, userID, f.id, kind, info.SessionID, info.AgentID); err != nil {
			return err
		}
		var lastLine, lastSnap int64
		if err := tx.QueryRow(ctx, `SELECT last_line_id, last_snapshot_id FROM derive_cursor WHERE user_id = $1 AND source_file_id = $2`,
			userID, f.id).Scan(&lastLine, &lastSnap); err != nil {
			return err
		}
		if kind == "other" { // advance so the file is not pending forever
			_, err := tx.Exec(ctx, `UPDATE derive_cursor SET
			    last_line_id = coalesce((SELECT max(id) FROM raw_lines WHERE source_file_id = $2), last_line_id),
			    last_snapshot_id = coalesce((SELECT max(id) FROM raw_snapshots WHERE source_file_id = $2), last_snapshot_id)
			  WHERE user_id = $1 AND source_file_id = $2`, userID, f.id)
			return err
		}
		b := newBatch(userID, f.id, info)
		var last int64
		if f.snapshot {
			var row rawRow
			err := tx.QueryRow(ctx, `SELECT id, device_id::text, content FROM raw_snapshots WHERE source_file_id = $1 ORDER BY id DESC LIMIT 1`, f.id).
				Scan(&row.id, &row.device, &row.data)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			if row.id <= lastSnap {
				return nil
			}
			evs, perr := parse.ParseSnapshot(info, row.data)
			if perr != nil {
				b.bad++
			}
			b.add(evs, 0)
			n, last = 1, row.id
			if err := b.flush(ctx, tx); err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `UPDATE derive_cursor SET last_snapshot_id = $3, last_ts = greatest(last_ts, $4) WHERE user_id = $1 AND source_file_id = $2`,
				userID, f.id, last, nullTime(b.maxTS))
			return err
		}
		var rows []rawRow
		r, err := tx.Query(ctx, `SELECT id, "offset", device_id::text, line FROM raw_lines WHERE source_file_id = $1 AND id > $2 ORDER BY id LIMIT $3`,
			f.id, lastLine, maxLines)
		if err != nil {
			return err
		}
		size := 0
		for r.Next() {
			var row rawRow
			if err := r.Scan(&row.id, &row.offset, &row.device, &row.data); err != nil {
				r.Close()
				return err
			}
			rows = append(rows, row)
			if size += len(row.data); size >= maxBytes {
				break
			}
		}
		r.Close()
		if err := r.Err(); err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			b.lines++
			b.devices[row.device]++
			evs, perr := parse.ParseLine(row.data)
			if perr != nil {
				b.bad++
				b.badOffset = max(b.badOffset, row.offset)
				continue
			}
			b.add(evs, row.offset)
		}
		n, last = len(rows), rows[len(rows)-1].id
		if b.bad > 0 { // counts only, never line content
			slog.Warn("derive: skipped unparsable lines", "user", userID, "file", f.id, "count", b.bad)
		}
		if err := b.flush(ctx, tx); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE derive_cursor SET last_line_id = $3, last_ts = greatest(last_ts, $4) WHERE user_id = $1 AND source_file_id = $2`,
			userID, f.id, last, nullTime(b.maxTS))
		return err
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}

// projectName is the last path segment of cwd; '/' and '\' both separate, trailing separators are ignored.
func projectName(cwd string) string {
	cwd = strings.TrimRight(cwd, `/\`)
	if i := strings.LastIndexAny(cwd, `/\`); i >= 0 {
		return cwd[i+1:]
	}
	return cwd
}
