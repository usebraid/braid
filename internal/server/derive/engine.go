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
	"github.com/usebraid/braid/internal/protocol"
	"github.com/usebraid/braid/internal/server/db"
	"github.com/usebraid/braid/internal/server/derive/parse"
)

// ParserVersion is bumped whenever a parser or engine change requires re-deriving from raw data.
const ParserVersion = 2

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
				// Spec 02 section 2: a data error must not stop the user's other files. Retry the batch row by
				// row so that only the rows the database still rejects are skipped.
				slog.Error("derive: the database rejected a batch, retrying it row by row", "user", userID, "file", f.id, "code", pgErr.Code, "table", pgErr.TableName)
				n, err = d.chunkByRow(ctx, userID, f)
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

func isDataError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (strings.HasPrefix(pgErr.Code, "22") || strings.HasPrefix(pgErr.Code, "23"))
}

// chunkByRow derives the next batch of the file one row per transaction. A row the database still
// rejects is counted in derive_errors and skipped; every other row of the batch is derived.
func (d *Deriver) chunkByRow(ctx context.Context, userID string, f pendingFile) (int, error) {
	limit := d.BatchLines
	if limit <= 0 {
		limit = 5000
	}
	if f.snapshot {
		limit = 1
	}
	total := 0
	for range limit {
		n, err := d.chunkN(ctx, userID, f, 1)
		if isDataError(err) {
			if err = d.skipRow(ctx, userID, f); err == nil {
				n = 1
			}
		}
		if err != nil {
			return total, err
		}
		if n == 0 {
			break
		}
		total += n
	}
	return total, nil
}

// skipRow counts a database error for the file in derive_errors and moves its cursor past the next row.
func (d *Deriver) skipRow(ctx context.Context, userID string, f pendingFile) error {
	kind, info := cursorKind(f)
	return d.DB.WithUser(ctx, userID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7002, hashtext($1::text))`, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO derive_cursor(user_id, source_file_id, kind, session_id, agent_id)
		  VALUES ($1, $2, $3, nullif($4, ''), nullif($5, '')) ON CONFLICT DO NOTHING`, userID, f.id, kind, info.SessionID, info.AgentID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE derive_cursor c SET
		    last_line_id = CASE WHEN $3 THEN c.last_line_id ELSE coalesce((SELECT min(id) FROM raw_lines WHERE source_file_id = $2 AND id > c.last_line_id), c.last_line_id) END,
		    last_snapshot_id = CASE WHEN $3 THEN coalesce((SELECT max(id) FROM raw_snapshots WHERE source_file_id = $2), c.last_snapshot_id) ELSE c.last_snapshot_id END
		  WHERE c.user_id = $1 AND c.source_file_id = $2`, userID, f.id, f.snapshot); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO derive_errors(user_id, source_file_id, errors, last_offset) VALUES ($1, $2, 1, 0)
		  ON CONFLICT (user_id, source_file_id) DO UPDATE SET errors = derive_errors.errors + 1`, userID, f.id)
		return err
	})
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
	size       int64
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
	return d.chunkN(ctx, userID, f, 0)
}

// chunkN is chunk with an explicit row limit (0: the configured batch size).
func (d *Deriver) chunkN(ctx context.Context, userID string, f pendingFile, maxLines int) (n int, err error) {
	maxBytes := d.BatchBytes
	if maxLines <= 0 {
		maxLines = d.BatchLines
	}
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
		if b.main || b.sub { // the open turn of the agent continues across batches
			var prev *time.Time
			err := tx.QueryRow(ctx, `SELECT work_prev, pending_ms, turn_asst FROM agents WHERE user_id = $1 AND session_id = $2 AND agent_id = $3`,
				userID, info.SessionID, info.AgentID).Scan(&prev, &b.at.pending, &b.at.asst)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			if prev != nil {
				b.at.prev = *prev
			}
		}
		var last int64
		if f.snapshot {
			var row rawRow
			err := tx.QueryRow(ctx, `SELECT id, device_id::text, octet_length(content), CASE WHEN octet_length(content) <= $2 THEN content END
			  FROM raw_snapshots WHERE source_file_id = $1 ORDER BY id DESC LIMIT 1`, f.id, protocol.MaxReadLine).
				Scan(&row.id, &row.device, &row.size, &row.data)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			if row.id <= lastSnap {
				return nil
			}
			if row.size > protocol.MaxReadLine {
				b.bad++
				b.tooLarge++
			} else {
				evs, perr := parse.ParseSnapshot(info, row.data)
				if perr != nil {
					b.bad++
				}
				b.add(evs, 0)
			}
			n, last = 1, row.id
			if err := b.flush(ctx, tx); err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `UPDATE derive_cursor SET last_snapshot_id = $3, last_ts = greatest(last_ts, $4) WHERE user_id = $1 AND source_file_id = $2`,
				userID, f.id, last, nullTime(b.maxTS))
			return err
		}
		var rows []rawRow
		r, err := tx.Query(ctx, `SELECT id, "offset", device_id::text, octet_length(line), CASE WHEN octet_length(line) <= $4 THEN line END
			  FROM raw_lines WHERE source_file_id = $1 AND id > $2 ORDER BY id LIMIT $3`,
			f.id, lastLine, maxLines, protocol.MaxReadLine)
		if err != nil {
			return err
		}
		size := 0
		for r.Next() {
			var row rawRow
			if err := r.Scan(&row.id, &row.offset, &row.device, &row.size, &row.data); err != nil {
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
			if row.size > protocol.MaxReadLine { // never loaded; raw stays intact
				b.bad++
				b.tooLarge++
				b.badOffset = max(b.badOffset, row.offset)
				continue
			}
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
			slog.Warn("derive: skipped unparsable lines", "user", userID, "file", f.id, "count", b.bad, "too_large", b.tooLarge)
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
