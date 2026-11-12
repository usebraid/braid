// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package state is the daemon's SQLite state store (spec §4.3).
package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // driver "sqlite"

	"github.com/usebraid/braid/internal/protocol"
)

const schema = `
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS files (
  rel_path      TEXT NOT NULL,
  head_hash     TEXT NOT NULL,
  kind          TEXT NOT NULL,
  acked_offset  INTEGER NOT NULL DEFAULT 0,
  content_hash  TEXT,
  file_id       TEXT,
  size_seen     INTEGER NOT NULL DEFAULT 0,
  spool_path    TEXT,
  original_gone INTEGER NOT NULL DEFAULT 0,
  last_ack_at   TEXT,
  device_scoped INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (rel_path, head_hash)
);`

// File is one row of the files table.
type File struct {
	RelPath, HeadHash string
	Kind              protocol.Kind
	AckedOffset       int64
	ContentHash       string
	FileID            string
	SizeSeen          int64
	SpoolPath         string
	OriginalGone      bool
	LastAckAt         time.Time // zero if never
	DeviceScoped      bool      // the server answered conflict: send with scope "device"
}

// DB wraps the SQLite connection pool.
type DB struct{ db *sql.DB }

// Open creates the file (0600), its parent directory (0700) and the schema.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if f, err := os.OpenFile(path, os.O_CREATE, 0o600); err != nil { //nolint:gosec // path is the daemon's own state file
		return nil, err
	} else if err := f.Close(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("state: schema: %w", err)
	}
	if err := addDeviceScoped(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("state: schema: %w", err)
	}
	return &DB{db: db}, nil
}

// addDeviceScoped upgrades a state DB created before the column existed.
func addDeviceScoped(db *sql.DB) error {
	rows, err := db.QueryContext(context.Background(), `PRAGMA table_info(files)`)
	if err != nil {
		return err
	}
	has := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			_ = rows.Close()
			return err
		}
		has = has || name == "device_scoped"
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil || has {
		return err
	}
	_, err = db.ExecContext(context.Background(), `ALTER TABLE files ADD COLUMN device_scoped INTEGER NOT NULL DEFAULT 0`)
	return err
}

// Close closes the database.
func (d *DB) Close() error { return d.db.Close() }

const cols = `rel_path, head_hash, kind, acked_offset, COALESCE(content_hash,''), COALESCE(file_id,''), size_seen, COALESCE(spool_path,''), original_gone, last_ack_at, device_scoped`

type scanner interface{ Scan(...any) error }

func scanFile(s scanner) (File, error) {
	var f File
	var kind string
	var gone, scoped int
	var at sql.NullString
	if err := s.Scan(&f.RelPath, &f.HeadHash, &kind, &f.AckedOffset, &f.ContentHash, &f.FileID, &f.SizeSeen, &f.SpoolPath, &gone, &at, &scoped); err != nil {
		return File{}, err
	}
	f.Kind, f.OriginalGone, f.DeviceScoped = protocol.Kind(kind), gone != 0, scoped != 0
	if at.Valid && at.String != "" {
		t, err := time.Parse(time.RFC3339, at.String)
		if err != nil {
			return File{}, err
		}
		f.LastAckAt = t.UTC()
	}
	return f, nil
}

// Get returns the row for (relPath, headHash).
func (d *DB) Get(ctx context.Context, relPath, headHash string) (File, bool, error) {
	f, err := scanFile(d.db.QueryRowContext(ctx, `SELECT `+cols+` FROM files WHERE rel_path=? AND head_hash=?`, relPath, headHash))
	if errors.Is(err, sql.ErrNoRows) {
		return File{}, false, nil
	}
	return f, err == nil, err
}

// Put upserts by (rel_path, head_hash).
func (d *DB) Put(ctx context.Context, f File) error {
	var at any
	if !f.LastAckAt.IsZero() {
		at = f.LastAckAt.UTC().Format(time.RFC3339)
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO files (`+cols0+`) VALUES (?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(rel_path, head_hash) DO UPDATE SET kind=excluded.kind, acked_offset=excluded.acked_offset,
 content_hash=excluded.content_hash, file_id=excluded.file_id, size_seen=excluded.size_seen,
 spool_path=excluded.spool_path, original_gone=excluded.original_gone, last_ack_at=excluded.last_ack_at,
 device_scoped=excluded.device_scoped`,
		f.RelPath, f.HeadHash, string(f.Kind), f.AckedOffset, f.ContentHash, f.FileID, f.SizeSeen, f.SpoolPath, f.OriginalGone, at, f.DeviceScoped)
	return err
}

const cols0 = `rel_path, head_hash, kind, acked_offset, content_hash, file_id, size_seen, spool_path, original_gone, last_ack_at, device_scoped`

// Delete removes a row; missing rows are not an error.
func (d *DB) Delete(ctx context.Context, relPath, headHash string) error {
	_, err := d.db.ExecContext(ctx, `DELETE FROM files WHERE rel_path=? AND head_hash=?`, relPath, headHash)
	return err
}

// List returns all rows ordered by key.
func (d *DB) List(ctx context.Context) ([]File, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT `+cols+` FROM files ORDER BY rel_path, head_hash`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Setting reads a setting.
func (d *DB) Setting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := d.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

// SetSetting upserts a setting.
func (d *DB) SetSetting(ctx context.Context, key, value string) error {
	_, err := d.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// ResetAcks forgets everything the server acknowledged (offsets, snapshot hashes, the resume marker)
// so the whole history is sent again to a new device. Spool paths and file ids stay.
func (d *DB) ResetAcks(ctx context.Context) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE files SET acked_offset=0, content_hash='', device_scoped=0, size_seen=CASE WHEN kind='snapshot' THEN -1 ELSE size_seen END`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key='resumed'`); err != nil {
		return err
	}
	return tx.Commit()
}

// Backlog summarises unacked work.
type Backlog struct {
	Files         int
	Bytes         int64
	OldestUnacked time.Time // min last_ack_at among backlog files; zero if none ever acked
	LastAck       time.Time // max last_ack_at over all files
}

// Backlog counts append files with acked_offset < size_seen and snapshot
// files flagged with size_seen = -1 (changed, not acked).
func (d *DB) Backlog(ctx context.Context) (Backlog, error) {
	all, err := d.List(ctx)
	if err != nil {
		return Backlog{}, err
	}
	var b Backlog
	for _, f := range all {
		if f.LastAckAt.After(b.LastAck) {
			b.LastAck = f.LastAckAt
		}
		switch {
		case f.Kind == protocol.KindAppend && f.AckedOffset < f.SizeSeen:
			b.Bytes += f.SizeSeen - f.AckedOffset
		case f.Kind == protocol.KindSnapshot && f.SizeSeen == -1:
		default:
			continue
		}
		b.Files++
		if !f.LastAckAt.IsZero() && (b.OldestUnacked.IsZero() || f.LastAckAt.Before(b.OldestUnacked)) {
			b.OldestUnacked = f.LastAckAt
		}
	}
	return b, nil
}
