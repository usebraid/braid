// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package derive turns raw transcript rows into the derived tables, exactly once per row.
package derive

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	rows, _, err = d.DeriveUserSessions(ctx, userID)
	return rows, err
}

// DeriveUserSessions is DeriveUser that also returns the distinct session ids of the files that had rows.
func (d *Deriver) DeriveUserSessions(ctx context.Context, userID string) (rows int, sessions []string, err error) {
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
		return 0, nil, err
	}
	for _, f := range files {
		for {
			n, err := d.chunk(ctx, userID, f)
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && (strings.HasPrefix(pgErr.Code, "22") || strings.HasPrefix(pgErr.Code, "23")) {
				// Spec 02 section 2: a data error on one file must not stop the user's other files.
				slog.Error("derive: skipping a batch the database rejected", "user", userID, "file", f.id, "code", pgErr.Code, "table", pgErr.TableName)
				if n, err = d.skipBatch(ctx, userID, f); err == nil {
					rows += n
					continue
				}
			}
			if err != nil {
				return rows, sessions, fmt.Errorf("derive file %d: %w", f.id, err)
			}
			if n == 0 {
				break
			}
			rows += n
			if sid := parse.ClassifyPath(f.rel).SessionID; sid != "" && !slices.Contains(sessions, sid) {
				sessions = append(sessions, sid)
			}
		}
	}
	return rows, sessions, nil
}

// skipBatch counts a database error for the file in derive_errors and moves its cursor past the batch
// that failed (the next maxLines rows; shortcut: coarser than the failing row, upgrade if lost lines matter).
func (d *Deriver) skipBatch(ctx context.Context, userID string, f pendingFile) (int, error) {
	maxLines := d.BatchLines
	if maxLines <= 0 {
		maxLines = 5000
	}
	kind, info := cursorKind(f)
	var n int
	err := d.DB.WithUser(ctx, userID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7002, hashtext($1::text))`, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO derive_cursor(user_id, source_file_id, kind, session_id, agent_id)
		  VALUES ($1, $2, $3, nullif($4, ''), nullif($5, '')) ON CONFLICT DO NOTHING`, userID, f.id, kind, info.SessionID, info.AgentID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `WITH c AS (SELECT last_line_id, last_snapshot_id FROM derive_cursor WHERE user_id = $1 AND source_file_id = $2),
		    l AS (SELECT id FROM raw_lines, c WHERE source_file_id = $2 AND id > c.last_line_id ORDER BY id LIMIT $3),
		    u AS (UPDATE derive_cursor SET
		      last_line_id = CASE WHEN $4 THEN last_line_id ELSE coalesce((SELECT max(id) FROM l), last_line_id) END,
		      last_snapshot_id = CASE WHEN $4 THEN coalesce((SELECT max(id) FROM raw_snapshots WHERE source_file_id = $2), last_snapshot_id) ELSE last_snapshot_id END
		      WHERE user_id = $1 AND source_file_id = $2 RETURNING 1)
		  SELECT CASE WHEN $4 THEN 1 ELSE (SELECT count(*) FROM l) END::int`, userID, f.id, maxLines, f.snapshot).Scan(&n); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO derive_errors(user_id, source_file_id, errors, last_offset) VALUES ($1, $2, 1, 0)
		  ON CONFLICT (user_id, source_file_id) DO UPDATE SET errors = derive_errors.errors + 1`, userID, f.id)
		return err
	})
	return n, err
}

// Reset deletes every derived row and cursor of the user (never raw data, never derive_state).
func (d *Deriver) Reset(ctx context.Context, userID string) error {
	return d.DB.WithUser(ctx, userID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7002, hashtext($1::text))`, userID); err != nil { // same lock as chunk
			return err
		}
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

func cursorKind(f pendingFile) (string, parse.FileInfo) {
	info := parse.ClassifyPath(f.rel)
	kind := map[parse.FileKind]string{parse.FileMain: "main", parse.FileSubagent: "subagent", parse.FileAgentMeta: "agent_meta", parse.FileCustomTitle: "custom_title"}[info.Kind]
	if kind == "" || f.snapshot != (info.Kind == parse.FileAgentMeta || info.Kind == parse.FileCustomTitle) {
		return "other", parse.FileInfo{}
	}
	return kind, info
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
	kind, info := cursorKind(f)
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
		var lastTS *time.Time
		if err := tx.QueryRow(ctx, `SELECT last_line_id, last_snapshot_id, last_ts FROM derive_cursor WHERE user_id = $1 AND source_file_id = $2`,
			userID, f.id).Scan(&lastLine, &lastSnap, &lastTS); err != nil {
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
		if lastTS != nil {
			b.prevTS = *lastTS
		}
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
