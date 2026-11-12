// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package syncer is the daemon's sync engine: scan, spool, read, batch, upload; state advances only on ACK.
package syncer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/usebraid/braid/internal/daemon/reader"
	"github.com/usebraid/braid/internal/daemon/scan"
	"github.com/usebraid/braid/internal/daemon/spool"
	"github.com/usebraid/braid/internal/daemon/state"
	"github.com/usebraid/braid/internal/protocol"
)

// Uploader is the server API the engine needs (satisfied by client.Client).
type Uploader interface {
	Ingest(ctx context.Context, req protocol.IngestRequest) (protocol.IngestResponse, error)
	Files(ctx context.Context) ([]protocol.RemoteFile, error)
}

// Engine runs sync passes.
type Engine struct {
	Root          string // Claude projects dir
	State         *state.DB
	Spool         *spool.Spool
	Up            Uploader
	MaxBatchBytes int // uncompressed line bytes per request, default 4 MiB
	MaxBatchLines int // default 5000
	Now           func() time.Time
}

// Result summarises a pass.
type Result struct {
	Uploaded     int64 // bytes acked in this pass
	PendingFiles int   // files still behind
}

type item struct {
	row     state.File
	abs     string // original path; "" if gone
	src     string // where to read from
	done    bool
	spooled bool // spool ensured this pass (snapshots)
	drain   bool // draining the retired spool entry of a replaced file; the new file follows
}

type sent struct {
	it   *item
	from int64
	hash string // snapshot content hash
}

// Pass runs until everything currently on disk is acked or an error occurs.
// Errors from Up are returned unchanged. A partial scan error is returned
// (wrapped) only when the pass otherwise succeeded.
func (e *Engine) Pass(ctx context.Context) (res Result, err error) {
	defer func() {
		if b, berr := e.State.Backlog(context.WithoutCancel(ctx)); berr == nil {
			res.PendingFiles = b.Files
		}
	}()
	if e.MaxBatchBytes <= 0 {
		e.MaxBatchBytes = 4 << 20
	}
	if e.MaxBatchLines <= 0 {
		e.MaxBatchLines = 5000
	}
	if e.Now == nil {
		e.Now = time.Now
	}
	if err := e.resume(ctx); err != nil {
		return res, err
	}
	entries, walkErr := scan.Walk(e.Root)
	if walkErr != nil && len(entries) == 0 {
		return res, walkErr // do not mistake an unreadable root for "all originals gone"
	}
	items, err := e.discover(ctx, entries)
	if err != nil {
		return res, err
	}
	for {
		req, metas, err := e.build(ctx, items)
		if err != nil {
			return res, err
		}
		if len(metas) == 0 {
			break
		}
		resp, err := e.Up.Ingest(ctx, req)
		if err != nil {
			return res, err
		}
		n, err := e.apply(ctx, resp, metas)
		res.Uploaded += n
		if err != nil {
			return res, err
		}
	}
	if walkErr != nil {
		return res, fmt.Errorf("syncer: scan skipped unreadable paths: %w", walkErr)
	}
	return res, nil
}

// resume seeds state from the server once, when no local state was ever built.
func (e *Engine) resume(ctx context.Context) error {
	if _, ok, err := e.State.Setting(ctx, "resumed"); err != nil || ok {
		return err
	}
	remote, err := e.Up.Files(ctx)
	if err != nil {
		return err
	}
	for _, r := range remote {
		if _, ok, err := e.State.Get(ctx, r.Path, r.HeadHash); err != nil {
			return err
		} else if ok {
			continue
		}
		if err := e.State.Put(ctx, state.File{RelPath: r.Path, HeadHash: r.HeadHash, Kind: r.Kind, AckedOffset: r.EndOffset, ContentHash: r.ContentHash}); err != nil {
			return err
		}
	}
	return e.State.SetSetting(ctx, "resumed", "1")
}

// discover merges disk entries and state rows into items. Rows whose original is gone read from the spool.
func (e *Engine) discover(ctx context.Context, entries []scan.Entry) ([]*item, error) {
	rows, err := e.State.List(ctx)
	if err != nil {
		return nil, err
	}
	byKey := map[[2]string]*item{}
	var items []*item
	for _, r := range rows {
		it := &item{row: r}
		byKey[[2]string{r.RelPath, r.HeadHash}] = it
		items = append(items, it)
	}
	for _, en := range entries {
		head := ""
		if en.Kind == protocol.KindAppend {
			h, ok, err := reader.HeadHash(en.AbsPath)
			if errors.Is(err, fs.ErrNotExist) || (err == nil && !ok) {
				continue // vanished, or no complete line yet
			}
			if err != nil {
				return nil, err
			}
			head = h
		}
		it := byKey[[2]string{en.RelPath, head}]
		if it == nil {
			it = &item{row: state.File{RelPath: en.RelPath, HeadHash: head, Kind: en.Kind}}
			byKey[[2]string{en.RelPath, head}] = it
			items = append(items, it)
		}
		it.abs = en.AbsPath
	}
	live := items[:0]
	for _, it := range items {
		orig := it.row
		if err := e.prepare(ctx, it); err != nil {
			return nil, err
		}
		if it.done {
			continue
		}
		if it.row != orig {
			if err := e.State.Put(ctx, it.row); err != nil {
				return nil, err
			}
		}
		live = append(live, it)
	}
	return live, nil
}

// prepare picks the read source, rewinds on truncation and spools unacked appends.
// Items without any source are dropped (done) and their rows deleted.
func (e *Engine) prepare(ctx context.Context, it *item) error {
	r := &it.row
	r.OriginalGone = it.abs == ""
	switch {
	case it.abs != "":
		it.src = it.abs
		if r.Kind == protocol.KindAppend && r.SpoolPath != "" && r.FileID != "" {
			if err := e.checkReplaced(it); err != nil {
				return err
			}
		}
	case r.SpoolPath != "" && fileExists(r.SpoolPath):
		it.src = r.SpoolPath
	default:
		it.done = true
		return e.drop(ctx, it)
	}
	if r.Kind != protocol.KindAppend {
		return nil
	}
	fi, err := os.Stat(it.src)
	if errors.Is(err, fs.ErrNotExist) {
		it.done = true // vanished since the walk; next pass decides
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Size() < r.AckedOffset {
		r.AckedOffset = 0 // shorter than acked, same head: resend, server dedupes
	}
	r.SizeSeen = fi.Size()
	if it.abs != "" && !it.drain && fi.Size() > r.AckedOffset {
		return e.ensureSpool(it)
	}
	return nil
}

// checkReplaced detects a file replaced under the same rel and head (new FileID) whose old
// content is not fully acked: the old spool entry is retired and drained first (state keeps one
// row per (rel, head): a SpoolPath that is not the live spool path marks the draining phase).
func (e *Engine) checkReplaced(it *item) error {
	r := &it.row
	if r.SpoolPath == e.Spool.Path(r.RelPath) {
		id, err := reader.FileID(it.abs)
		if err != nil {
			return nil //nolint:nilerr // vanished: next pass decides
		}
		if id == r.FileID {
			return nil
		}
		if lines, _, err := reader.ReadLines(r.SpoolPath, r.AckedOffset, 1); err != nil || len(lines) == 0 {
			return nil //nolint:nilerr // nothing unacked in the old entry (or it is gone): Ensure replaces it
		}
		old, err := e.Spool.Retire(r.RelPath, r.FileID)
		if err != nil || old == "" {
			return err
		}
		r.SpoolPath = old
	}
	if !fileExists(r.SpoolPath) {
		r.SpoolPath = ""
		return nil
	}
	it.drain, it.src = true, r.SpoolPath
	return nil
}

func (e *Engine) ensureSpool(it *item) error {
	id, err := reader.FileID(it.abs)
	if err != nil {
		return err
	}
	p, _, err := e.Spool.Ensure(it.row.RelPath, it.abs, id)
	if err != nil {
		return err
	}
	it.row.SpoolPath, it.row.FileID, it.spooled = p, id, true
	return nil
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// drop removes the spool entry and the row of an item that is done and has no original.
func (e *Engine) drop(ctx context.Context, it *item) error {
	if it.row.SpoolPath != "" {
		if err := e.Spool.Remove(it.row.SpoolPath); err != nil {
			return err
		}
	}
	return e.State.Delete(ctx, it.row.RelPath, it.row.HeadHash)
}

// finish handles a fully acked item: gone → forget it; present → release the spool entry.
func (e *Engine) finish(ctx context.Context, it *item) error {
	if it.drain { // old file drained: switch the same row over to the new file
		if err := e.Spool.Remove(it.row.SpoolPath); err != nil {
			return err
		}
		r := &it.row
		r.SpoolPath, r.FileID, r.AckedOffset = "", "", 0
		it.drain, it.src = false, it.abs
		fi, err := os.Stat(it.abs)
		if err != nil {
			it.done = true // vanished; next pass decides
			return e.State.Put(ctx, *r)
		}
		r.SizeSeen = fi.Size()
		if fi.Size() > 0 {
			if err := e.ensureSpool(it); err != nil {
				return err
			}
		}
		return e.State.Put(ctx, *r)
	}
	it.done = true
	if it.abs == "" {
		return e.drop(ctx, it)
	}
	if it.row.SpoolPath == "" {
		return nil
	}
	if err := e.Spool.Remove(it.row.SpoolPath); err != nil {
		return err
	}
	it.row.SpoolPath = ""
	return e.State.Put(ctx, it.row)
}

// build reads the next batch across all unfinished items.
func (e *Engine) build(ctx context.Context, items []*item) (protocol.IngestRequest, []sent, error) {
	var req protocol.IngestRequest
	var metas []sent
	used, nlines := 0, 0
	for _, it := range items {
	retry:
		if it.done {
			continue
		}
		rem := e.MaxBatchBytes - used
		if nlines >= e.MaxBatchLines || rem <= 0 {
			break
		}
		r := it.row
		if r.Kind == protocol.KindSnapshot {
			content, hash, err := reader.Snapshot(it.src)
			if errors.Is(err, fs.ErrNotExist) {
				it.done = true
				continue
			}
			if err != nil {
				return req, nil, err
			}
			if hash == r.ContentHash {
				if r.SizeSeen != 0 {
					it.row.SizeSeen = 0
					if err := e.State.Put(ctx, it.row); err != nil {
						return req, nil, err
					}
				}
				if err := e.finish(ctx, it); err != nil {
					return req, nil, err
				}
				continue
			}
			if len(metas) > 0 && len(content) > rem {
				break // goes alone in the next batch
			}
			if it.abs != "" && !it.spooled {
				if err := e.ensureSpool(it); err != nil {
					return req, nil, err
				}
			}
			if it.row.SizeSeen != -1 || it.row.SpoolPath != r.SpoolPath {
				it.row.SizeSeen = -1
				if err := e.State.Put(ctx, it.row); err != nil {
					return req, nil, err
				}
			}
			c := protocol.EncodeLine(content)
			req.Files = append(req.Files, protocol.FileBatch{Kind: protocol.KindSnapshot, Path: r.RelPath, Content: &c})
			metas = append(metas, sent{it: it, hash: hash})
			used += len(content)
			nlines++
			continue
		}
		lines, next, err := reader.ReadLines(it.src, r.AckedOffset, rem)
		if errors.Is(err, fs.ErrNotExist) {
			it.done = true
			continue
		}
		if err != nil {
			return req, nil, err
		}
		if len(lines) == 0 {
			wasDrain := it.drain
			if err := e.finish(ctx, it); err != nil {
				return req, nil, err
			}
			if wasDrain {
				goto retry // now the new file, same row
			}
			continue
		}
		if len(metas) > 0 && next-r.AckedOffset > int64(rem) {
			break // oversized first line goes alone
		}
		if k := e.MaxBatchLines - nlines; len(lines) > k {
			lines = lines[:k]
			next = protocol.EndOffset(r.AckedOffset, lines)
		}
		fb := protocol.FileBatch{Kind: protocol.KindAppend, Path: r.RelPath, HeadHash: r.HeadHash, FromOffset: r.AckedOffset}
		for _, l := range lines {
			fb.Lines = append(fb.Lines, protocol.EncodeLine(l))
		}
		req.Files = append(req.Files, fb)
		metas = append(metas, sent{it: it, from: r.AckedOffset})
		used += int(next - r.AckedOffset)
		nlines += len(lines)
	}
	return req, metas, nil
}

// apply advances state from the server's verdicts; only ok/gap move anything.
func (e *Engine) apply(ctx context.Context, resp protocol.IngestResponse, metas []sent) (int64, error) {
	byKey := map[[2]string]sent{}
	for _, m := range metas {
		byKey[[2]string{m.it.row.RelPath, m.it.row.HeadHash}] = m
	}
	var uploaded int64
	seen := 0
	for _, fr := range resp.Files {
		m, ok := byKey[[2]string{fr.Path, fr.HeadHash}]
		if !ok {
			continue
		}
		seen++
		r := &m.it.row
		switch fr.Status {
		case protocol.StatusOK:
			if r.Kind == protocol.KindSnapshot {
				r.ContentHash, r.SizeSeen = m.hash, 0
			} else {
				uploaded += fr.EndOffset - m.from
				r.AckedOffset = fr.EndOffset
			}
			r.LastAckAt = e.Now()
		case protocol.StatusGap:
			if fr.EndOffset >= m.from {
				return uploaded, fmt.Errorf("syncer: server reported gap at %d for offset %d", fr.EndOffset, m.from)
			}
			r.AckedOffset = fr.EndOffset // rewind; the next loop re-reads from there
		default:
			return uploaded, fmt.Errorf("syncer: unknown status %q", fr.Status)
		}
		if err := e.State.Put(ctx, *r); err != nil {
			return uploaded, err
		}
	}
	if seen != len(metas) {
		return uploaded, errors.New("syncer: server response misses files")
	}
	return uploaded, nil
}
