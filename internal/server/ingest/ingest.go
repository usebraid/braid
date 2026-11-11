// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package ingest

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/usebraid/braid/internal/protocol"
	"github.com/usebraid/braid/internal/server/db"
	"github.com/usebraid/braid/internal/server/httpx"
)

// Handler serves the ingest endpoints.
type Handler struct{ db *db.DB }

// New returns a Handler on d.
func New(d *db.DB) *Handler { return &Handler{db: d} }

// Register mounts the endpoints; both need scope ingest:write.
func (h *Handler) Register(mux *http.ServeMux, lim *httpx.Limiter) {
	mux.Handle("POST /v1/ingest", RequireDevice(h.db, "ingest:write", lim, http.HandlerFunc(h.ingest)))
	mux.Handle("GET /v1/files", RequireDevice(h.db, "ingest:write", lim, http.HandlerFunc(h.files)))
}

var errKindMismatch = errors.New("kind mismatch")

func (h *Handler) ingest(w http.ResponseWriter, r *http.Request) {
	dev, _ := DeviceFrom(r.Context())
	if r.Header.Get("Content-Encoding") != "gzip" {
		errJSON(w, http.StatusUnsupportedMediaType, "unsupported_encoding")
		return
	}
	body, status, code := readBody(w, r)
	if code != "" {
		errJSON(w, status, code)
		return
	}
	var req protocol.IngestRequest
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || req.Validate() != nil {
		errJSON(w, http.StatusBadRequest, "bad_request")
		return
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		errJSON(w, http.StatusBadRequest, "bad_request")
		return
	}
	resp := protocol.IngestResponse{Files: make([]protocol.FileResult, 0, len(req.Files))}
	err := h.db.WithUser(r.Context(), dev.UserID, func(tx pgx.Tx) error {
		resp.Files = resp.Files[:0]
		for _, f := range req.Files {
			res, err := storeFile(r, tx, dev, f)
			if err != nil {
				return err
			}
			resp.Files = append(resp.Files, res)
		}
		_, err := tx.Exec(r.Context(), `UPDATE devices SET last_sync_at = now() WHERE id = $1`, dev.DeviceID)
		return err
	})
	switch {
	case errors.Is(err, errKindMismatch):
		errJSON(w, http.StatusBadRequest, "bad_request")
	case err != nil:
		slog.Error("ingest", "err", err) // pgx errors carry no line contents
		errJSON(w, http.StatusInternalServerError, "internal")
	default:
		httpx.WriteJSON(w, http.StatusOK, resp)
	}
}

// readBody returns the decompressed body, or an HTTP status and error code.
func readBody(w http.ResponseWriter, r *http.Request) ([]byte, int, string) {
	zr, err := gzip.NewReader(http.MaxBytesReader(w, r.Body, protocol.MaxCompressedBody))
	if err != nil {
		return nil, statusFor(err), codeFor(err)
	}
	defer func() { _ = zr.Close() }()
	b, err := io.ReadAll(io.LimitReader(zr, protocol.MaxDecompressedBody+1))
	if err != nil {
		return nil, statusFor(err), codeFor(err)
	}
	if len(b) > protocol.MaxDecompressedBody {
		return nil, http.StatusRequestEntityTooLarge, "too_large"
	}
	return b, 0, ""
}

func statusFor(err error) int {
	if codeFor(err) == "too_large" {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}

func codeFor(err error) string {
	var mb *http.MaxBytesError
	if errors.As(err, &mb) {
		return "too_large"
	}
	return "bad_request"
}

func storeFile(r *http.Request, tx pgx.Tx, dev Device, f protocol.FileBatch) (protocol.FileResult, error) {
	ctx := r.Context()
	res := protocol.FileResult{Path: f.Path, HeadHash: f.HeadHash, Status: protocol.StatusOK}
	var id, end int64
	var kind string
	// The conflict update takes the row lock, serialising concurrent writers of the same file.
	err := tx.QueryRow(ctx, `
		INSERT INTO source_files(user_id, device_id, rel_path, head_hash, kind, first_seen, last_seen)
		VALUES ($1, $2, $3, $4, $5, now(), now())
		ON CONFLICT (user_id, device_id, rel_path, head_hash) DO UPDATE SET last_seen = now()
		RETURNING id, kind, end_offset`,
		dev.UserID, dev.DeviceID, f.Path, f.HeadHash, string(f.Kind)).Scan(&id, &kind, &end)
	if err != nil {
		return res, err
	}
	if kind != string(f.Kind) {
		return res, errKindMismatch
	}
	if f.Kind == protocol.KindSnapshot {
		c, err := f.Content.Bytes()
		if err != nil {
			return res, err
		}
		h := sha256.Sum256(c)
		_, err = tx.Exec(ctx, `INSERT INTO raw_snapshots(user_id, source_file_id, content_hash, content)
			VALUES ($1, $2, $3, $4) ON CONFLICT (user_id, source_file_id, content_hash) DO NOTHING`,
			dev.UserID, id, h[:], c)
		return res, err
	}
	if f.FromOffset > end {
		res.Status, res.EndOffset = protocol.StatusGap, end
		return res, nil
	}
	offs := make([]int64, len(f.Lines))
	hashes := make([][]byte, len(f.Lines))
	lines := make([][]byte, len(f.Lines))
	cur := f.FromOffset
	for i, l := range f.Lines {
		b, err := l.Bytes()
		if err != nil {
			return res, err
		}
		h := sha256.Sum256(b)
		offs[i], hashes[i], lines[i] = cur, h[:], b
		cur = protocol.EndOffset(cur, [][]byte{b})
	}
	res.EndOffset = cur
	// One statement for all lines: imports send thousands.
	_, err = tx.Exec(ctx, `
		INSERT INTO raw_lines(user_id, source_file_id, "offset", line_hash, line)
		SELECT $1, $2, t.o, t.h, t.l FROM unnest($3::bigint[], $4::bytea[], $5::bytea[]) AS t(o, h, l)
		ON CONFLICT (user_id, source_file_id, "offset", line_hash) DO NOTHING`,
		dev.UserID, id, offs, hashes, lines)
	if err != nil {
		return res, err
	}
	_, err = tx.Exec(ctx, `UPDATE source_files SET end_offset = greatest(end_offset, $2) WHERE id = $1`, id, res.EndOffset)
	return res, err
}

func (h *Handler) files(w http.ResponseWriter, r *http.Request) {
	dev, _ := DeviceFrom(r.Context())
	out := []protocol.RemoteFile{}
	err := h.db.WithUser(r.Context(), dev.UserID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `
			SELECT f.rel_path, f.head_hash, f.kind, f.end_offset, coalesce(encode(s.content_hash, 'hex'), '')
			  FROM source_files f
			  LEFT JOIN LATERAL (SELECT content_hash FROM raw_snapshots WHERE source_file_id = f.id
			                      ORDER BY received_at DESC, id DESC LIMIT 1) s ON true
			 WHERE f.device_id = $1 ORDER BY f.rel_path, f.head_hash`, dev.DeviceID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var f protocol.RemoteFile
			if err := rows.Scan(&f.Path, &f.HeadHash, &f.Kind, &f.EndOffset, &f.ContentHash); err != nil {
				return err
			}
			out = append(out, f)
		}
		return rows.Err()
	})
	if err != nil {
		slog.Error("files", "err", err)
		errJSON(w, http.StatusInternalServerError, "internal")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
