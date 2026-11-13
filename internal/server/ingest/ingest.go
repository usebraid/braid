// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package ingest

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/usebraid/braid/internal/protocol"
	"github.com/usebraid/braid/internal/server/db"
	"github.com/usebraid/braid/internal/server/httpx"
	"github.com/usebraid/braid/internal/server/live"
)

// Handler serves the ingest endpoints.
type Handler struct {
	db  *db.DB
	sem chan struct{} // bounds concurrent ingest requests (memory)
	// Per-device in-flight limit, taken before the body is read: a device that stalls its
	// bodies can only block itself, not the global slots.
	mu       sync.Mutex
	inflight map[string]int
}

// MaxIngestPerDevice bounds in-flight ingest requests per device (one is the daemon's normal case).
const MaxIngestPerDevice = 2

func (h *Handler) enter(dev string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.inflight[dev] >= MaxIngestPerDevice {
		return false
	}
	h.inflight[dev]++
	return true
}

func (h *Handler) leave(dev string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.inflight[dev]--; h.inflight[dev] <= 0 {
		delete(h.inflight, dev)
	}
}

// MaxConcurrentIngest bounds in-flight ingest requests per server.
const MaxConcurrentIngest = 4

// New returns a Handler on d.
func New(d *db.DB) *Handler {
	return &Handler{db: d, sem: make(chan struct{}, MaxConcurrentIngest), inflight: map[string]int{}}
}

// Register mounts the endpoints; both need scope ingest:write.
func (h *Handler) Register(mux *http.ServeMux, lim *httpx.Limiter) {
	mux.Handle("POST /v1/ingest", RequireDevice(h.db, "ingest:write", lim, http.HandlerFunc(h.ingest)))
	mux.Handle("GET /v1/files", RequireDevice(h.db, "ingest:write", lim, http.HandlerFunc(h.files)))
}

var (
	errKindMismatch = errors.New("kind mismatch")
	errBadParts     = errors.New("assembled line does not match its length or hash")
)

// storePart upserts one chunk of the line at offset and, once all chunks of that line are present,
// assembles and verifies it, deletes the parts and returns the whole line.
func storePart(ctx context.Context, tx pgx.Tx, id, offset int64, p *protocol.Part, chunk []byte) (whole []byte, done bool, err error) {
	sum, err := hex.DecodeString(p.SHA256)
	if err != nil {
		return nil, false, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM raw_line_parts WHERE received_at < now() - interval '7 days'`); err != nil {
		return nil, false, err
	}
	// Chunks of a different line at this offset (changed content) can never complete this one.
	if _, err = tx.Exec(ctx, `DELETE FROM raw_line_parts WHERE source_file_id = $1 AND "offset" = $2 AND sha256 <> $3`, id, offset, sum); err != nil {
		return nil, false, err
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO raw_line_parts(user_id, source_file_id, "offset", idx, count, total_len, sha256, chunk)
		VALUES (app_user(), $1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (user_id, source_file_id, "offset", idx)
		DO UPDATE SET count = excluded.count, total_len = excluded.total_len, sha256 = excluded.sha256, chunk = excluded.chunk, received_at = now()`,
		id, offset, p.Index, p.Count, p.TotalLen, sum, chunk); err != nil {
		return nil, false, err
	}
	var have int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM raw_line_parts WHERE source_file_id = $1 AND "offset" = $2 AND sha256 = $3 AND count = $4`,
		id, offset, sum, p.Count).Scan(&have); err != nil || have < p.Count {
		return nil, false, err
	}
	rows, err := tx.Query(ctx, `SELECT chunk FROM raw_line_parts WHERE source_file_id = $1 AND "offset" = $2 AND sha256 = $3 ORDER BY idx`, id, offset, sum)
	if err != nil {
		return nil, false, err
	}
	for rows.Next() {
		var c []byte
		if err = rows.Scan(&c); err != nil {
			break
		}
		whole = append(whole, c...)
	}
	rows.Close()
	if err == nil {
		err = rows.Err()
	}
	if err != nil {
		return nil, false, err
	}
	if h := sha256.Sum256(whole); int64(len(whole)) != p.TotalLen || !bytes.Equal(h[:], sum) {
		return nil, false, errBadParts
	}
	_, err = tx.Exec(ctx, `DELETE FROM raw_line_parts WHERE source_file_id = $1 AND "offset" = $2`, id, offset)
	return whole, err == nil, err
}

func (h *Handler) ingest(w http.ResponseWriter, r *http.Request) {
	dev, _ := DeviceFrom(r.Context())
	if r.Header.Get("Content-Encoding") != "gzip" {
		errJSON(w, http.StatusUnsupportedMediaType, "unsupported_encoding")
		return
	}
	if !h.enter(dev.DeviceID) {
		httpx.TooMany(w, time.Second)
		return
	}
	defer h.leave(dev.DeviceID)
	// Global slot before decoding: the decoded body is what the slots bound.
	select {
	case h.sem <- struct{}{}:
		defer func() { <-h.sem }()
	default:
		httpx.TooMany(w, time.Second)
		return
	}
	var req protocol.IngestRequest
	if status, code := readRequest(w, r, &req); code != "" {
		errJSON(w, status, code)
		return
	}
	// Fixed lock order across concurrent requests avoids deadlocks; results go back in request order.
	order := make([]int, len(req.Files))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool {
		a, b := req.Files[order[i]], req.Files[order[j]]
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Path, b.Path), cmp.Compare(a.HeadHash, b.HeadHash), cmp.Compare(a.Scope, b.Scope)) < 0
	})
	resp := protocol.IngestResponse{Files: make([]protocol.FileResult, len(req.Files))}
	err := h.db.WithUser(r.Context(), dev.UserID, func(tx pgx.Tx) error {
		for _, i := range order {
			res, err := storeFile(r, tx, dev, req.Files[i])
			if err != nil {
				return err
			}
			resp.Files[i] = res
		}
		if _, err := tx.Exec(r.Context(), `UPDATE devices SET last_sync_at = now() WHERE id = $1`, dev.DeviceID); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `SELECT pg_notify('braid_ingest', $1)`, dev.UserID); err != nil { // delivered at commit
			return err
		}
		synced, _ := json.Marshal(live.Event{UserID: dev.UserID, Type: "device_synced", DeviceID: dev.DeviceID})
		_, err := tx.Exec(r.Context(), `SELECT pg_notify($1, $2)`, live.ChannelSynced, string(synced))
		return err
	})
	switch {
	case errors.Is(err, errKindMismatch), errors.Is(err, errBadParts):
		errJSON(w, http.StatusBadRequest, "bad_request")
	case err != nil:
		slog.Error("ingest", "err", err) // pgx errors carry no line contents
		errJSON(w, http.StatusInternalServerError, "internal")
	default:
		httpx.WriteJSON(w, http.StatusOK, resp)
	}
}

var errTooLarge = errors.New("body too large")

// capReader fails with errTooLarge once more than n bytes were read.
type capReader struct {
	r io.Reader
	n int64
}

func (c *capReader) Read(p []byte) (int, error) {
	if c.n < 0 {
		return 0, errTooLarge
	}
	if int64(len(p)) > c.n+1 {
		p = p[:c.n+1]
	}
	k, err := c.r.Read(p)
	c.n -= int64(k)
	if c.n < 0 {
		return k, errTooLarge
	}
	return k, err
}

// readRequest streams gzip -> JSON into req and validates it; code is "" on success.
func readRequest(w http.ResponseWriter, r *http.Request, req *protocol.IngestRequest) (status int, code string) {
	fail := func(err error) (int, string) {
		var mb *http.MaxBytesError
		if errors.Is(err, errTooLarge) || errors.As(err, &mb) || errors.Is(err, protocol.ErrTooManyLines) || errors.Is(err, protocol.ErrTooManyFiles) {
			return http.StatusRequestEntityTooLarge, "too_large"
		}
		return http.StatusBadRequest, "bad_request"
	}
	zr, err := gzip.NewReader(http.MaxBytesReader(w, r.Body, protocol.MaxCompressedBody))
	if err != nil {
		return fail(err)
	}
	defer func() { _ = zr.Close() }()
	dec := json.NewDecoder(&capReader{r: zr, n: protocol.MaxDecompressedBody})
	dec.DisallowUnknownFields()
	if err := dec.Decode(req); err != nil {
		return fail(err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("trailing data")
		}
		return fail(err)
	}
	if err := req.Validate(); err != nil {
		return fail(err)
	}
	return 0, ""
}

func storeFile(r *http.Request, tx pgx.Tx, dev Device, f protocol.FileBatch) (protocol.FileResult, error) {
	ctx := r.Context()
	res := protocol.FileResult{Path: f.Path, HeadHash: f.HeadHash, Status: protocol.StatusOK}
	var id, end int64
	var kind string
	var scope *string // NULL: per-user identity; the caller's device: device-scoped fallback
	if f.Scope == protocol.ScopeDevice {
		scope = &dev.DeviceID
	}
	// The conflict update takes the row lock, serialising concurrent writers of the same file.
	err := tx.QueryRow(ctx, `
		INSERT INTO source_files(user_id, scope_device_id, rel_path, head_hash, kind, first_seen, last_seen)
		VALUES ($1, $2, $3, $4, $5, now(), now())
		ON CONFLICT ON CONSTRAINT source_files_identity DO UPDATE SET last_seen = now()
		RETURNING id, kind, end_offset`,
		dev.UserID, scope, f.Path, f.HeadHash, string(f.Kind)).Scan(&id, &kind, &end)
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
		if p := f.Content.Part; p != nil {
			whole, done, err := storePart(ctx, tx, id, 0, p, c)
			if err != nil {
				return res, err
			}
			if !done {
				res.Status, res.EndOffset = protocol.StatusPending, end
				return res, nil
			}
			c = whole
		}
		h := sha256.Sum256(c)
		// Append-only: skip only if the newest row already has this hash (the file row lock serialises this).
		_, err = tx.Exec(ctx, `INSERT INTO raw_snapshots(user_id, source_file_id, content_hash, content, received_at, device_id)
			SELECT $1, $2, $3, $4, clock_timestamp(), $5::uuid
			 WHERE NOT EXISTS (SELECT 1 FROM (SELECT content_hash FROM raw_snapshots WHERE source_file_id = $2
			                    ORDER BY received_at DESC, id DESC LIMIT 1) n WHERE n.content_hash = $3)`,
			dev.UserID, id, h[:], c, dev.DeviceID)
		return res, err
	}
	if f.FromOffset > end && !f.AllowGap {
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
		if l.Part != nil { // Validate allows a part only as the sole line
			var stored bool // a resend after a lost ack: the line is already complete
			if sum, err := hex.DecodeString(l.Part.SHA256); err != nil {
				return res, err
			} else if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM raw_lines WHERE source_file_id = $1 AND "offset" = $2 AND line_hash = $3)`, id, cur, sum).Scan(&stored); err != nil {
				return res, err
			} else if stored {
				res.EndOffset = cur + l.Part.TotalLen + 1
				return res, nil
			}
			whole, done, err := storePart(ctx, tx, id, cur, l.Part, b)
			if err != nil {
				return res, err
			}
			if !done {
				res.Status, res.EndOffset = protocol.StatusPending, end
				return res, nil
			}
			b = whole
		}
		h := sha256.Sum256(b)
		offs[i], hashes[i], lines[i] = cur, h[:], b
		cur = protocol.EndOffset(cur, [][]byte{b})
	}
	if scope == nil && f.FromOffset < end {
		// Another device holds a different line at an offset this batch writes: two machines appended
		// to the same synced file. Store nothing; the daemon falls back to a device-scoped identity.
		var clash bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM unnest($2::bigint[], $3::bytea[]) t(o, h)
			  JOIN raw_lines l ON l.source_file_id = $1 AND l."offset" = t.o
			 WHERE l.device_id <> $4::uuid
			   AND NOT EXISTS (SELECT 1 FROM raw_lines m WHERE m.source_file_id = $1 AND m."offset" = t.o AND m.line_hash = t.h))`,
			id, offs, hashes, dev.DeviceID).Scan(&clash); err != nil {
			return res, err
		}
		if clash {
			res.Status, res.EndOffset = protocol.StatusConflict, end
			return res, nil
		}
	}
	res.EndOffset = cur
	// One statement for all lines: imports send thousands.
	_, err = tx.Exec(ctx, `
		INSERT INTO raw_lines(user_id, source_file_id, "offset", line_hash, line, device_id)
		SELECT $1, $2, t.o, t.h, t.l, $6::uuid FROM unnest($3::bigint[], $4::bytea[], $5::bytea[]) AS t(o, h, l)
		ON CONFLICT (user_id, source_file_id, "offset", line_hash) DO NOTHING`,
		dev.UserID, id, offs, hashes, lines, dev.DeviceID)
	if err != nil {
		return res, err
	}
	_, err = tx.Exec(ctx, `UPDATE source_files SET end_offset = greatest(end_offset, $2) WHERE id = $1`, id, res.EndOffset)
	return res, err
}

func (h *Handler) files(w http.ResponseWriter, r *http.Request) {
	dev, _ := DeviceFrom(r.Context())
	var after int64
	if a := r.URL.Query().Get("after"); a != "" {
		var err error
		if after, err = strconv.ParseInt(a, 10, 64); err != nil || after < 0 {
			errJSON(w, http.StatusBadRequest, "bad_request")
			return
		}
	}
	limit := protocol.DefaultFilesLimit
	if l := r.URL.Query().Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 {
			errJSON(w, http.StatusBadRequest, "bad_request")
			return
		}
		limit = min(n, protocol.MaxFilesLimit)
	}
	out := protocol.FilesPage{Files: []protocol.RemoteFile{}}
	var ids []int64
	err := h.db.WithUser(r.Context(), dev.UserID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `
			SELECT f.id, f.rel_path, f.head_hash, f.kind, f.end_offset, coalesce(encode(s.content_hash, 'hex'), ''),
			       f.scope_device_id IS NOT NULL, coalesce(t."offset", 0), coalesce(encode(t.line_hash, 'hex'), '')
			  FROM source_files f
			  LEFT JOIN LATERAL (SELECT content_hash FROM raw_snapshots WHERE source_file_id = f.id
			                      ORDER BY received_at DESC, id DESC LIMIT 1) s ON true
			  LEFT JOIN LATERAL (SELECT "offset", line_hash FROM raw_lines WHERE source_file_id = f.id
			                      ORDER BY "offset" DESC, id DESC LIMIT 1) t ON true
			 WHERE (f.scope_device_id IS NULL OR f.scope_device_id = $1) AND f.id > $2
			 ORDER BY f.id LIMIT $3`, dev.DeviceID, after, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var f protocol.RemoteFile
			var id int64
			var scoped bool
			if err := rows.Scan(&id, &f.Path, &f.HeadHash, &f.Kind, &f.EndOffset, &f.ContentHash, &scoped, &f.TailOffset, &f.TailHash); err != nil {
				return err
			}
			if scoped {
				f.Scope = protocol.ScopeDevice
			}
			out.Files = append(out.Files, f)
			ids = append(ids, id)
		}
		return rows.Err()
	})
	if err != nil {
		slog.Error("files", "err", err)
		errJSON(w, http.StatusInternalServerError, "internal")
		return
	}
	if len(out.Files) > limit {
		out.Files = out.Files[:limit]
		out.Next = strconv.FormatInt(ids[limit-1], 10)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
