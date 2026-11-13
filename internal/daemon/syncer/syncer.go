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
	"path/filepath"
	"slices"
	"time"

	"github.com/usebraid/braid/internal/daemon/client"
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
	MaxLine       int // lines and snapshots above this many bytes are never uploaded, only recorded as skipped; default protocol.MaxLineBytes

	parts    map[partKey]*partState // chunks of oversized lines in flight, kept across passes so a retry resumes
	fileErrs []error                // per-file failures of the running pass; the pass goes on without those files
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
	part *partRef
}

// partKey identifies one oversized line (offset) or snapshot (offset 0) by content.
type partKey struct {
	rel, head string
	off       int64
	sum       string
}

type partState struct {
	next   int // index of the next chunk to send; chunks before it are stored on the server
	chunks []protocol.Line
}

type partRef struct {
	key partKey
	idx int
}

// nextPart returns the next chunk of raw to send for key.
func (e *Engine) nextPart(key partKey, raw []byte) (protocol.Line, int) {
	if e.parts == nil {
		e.parts = map[partKey]*partState{}
	}
	ps := e.parts[key]
	if ps == nil {
		ps = &partState{chunks: protocol.SplitLine(raw, protocol.ChunkSize)}
		e.parts[key] = ps
	}
	return ps.chunks[ps.next], ps.next
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
	if e.MaxLine <= 0 {
		e.MaxLine = protocol.MaxLineBytes
	}
	e.fileErrs = nil
	defer func() { // visible to `braid status`; "" when nothing is stuck
		msg := ""
		if n := len(e.fileErrs); n > 0 {
			msg = fmt.Sprintf("%d file(s) cannot be synced, first: %v", n, e.fileErrs[0])
		}
		_ = e.State.SetSetting(context.WithoutCancel(ctx), "blocked", msg)
	}()
	// A failing resume must not stop collection: spooling is unconditional (§4.5), so the error
	// is returned only after discover has spooled everything.
	resumeErr := e.resume(ctx)
	entries, walkErr := scan.Walk(e.Root)
	if walkErr != nil && len(entries) == 0 {
		return res, walkErr // do not mistake an unreadable root for "all originals gone"
	}
	items, err := e.discover(ctx, entries)
	if err != nil {
		return res, err
	}
	if resumeErr != nil {
		return res, resumeErr
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
		var perm *client.PermanentError
		if errors.As(err, &perm) {
			// The server rejects the whole request; find the file(s) that cause it, block those, keep the rest.
			resp, metas, err = e.isolate(ctx, req, metas)
			if err != nil { // retryable: keep the files that were already acked one by one
				n, _ := e.apply(ctx, resp, metas)
				res.Uploaded += n
				return res, err
			}
		}
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
		e.fileErrs = append(e.fileErrs, fmt.Errorf("syncer: scan skipped unreadable paths: %w", walkErr))
	}
	return res, errors.Join(e.fileErrs...)
}

// isolate re-sends a rejected request one file at a time. Files the server still rejects permanently
// are blocked for this pass (recorded in fileErrs); the others are returned with their responses.
func (e *Engine) isolate(ctx context.Context, req protocol.IngestRequest, metas []sent) (protocol.IngestResponse, []sent, error) {
	var out protocol.IngestResponse
	var okMetas []sent
	for i, m := range metas {
		resp, err := e.Up.Ingest(ctx, protocol.IngestRequest{Files: req.Files[i : i+1]})
		var perm *client.PermanentError
		if errors.As(err, &perm) {
			m.it.done = true
			e.fileErrs = append(e.fileErrs, fmt.Errorf("syncer: server rejects %s: %w", m.it.row.RelPath, err))
			continue
		}
		if err != nil {
			return out, okMetas, err
		}
		out.Files = append(out.Files, resp.Files...)
		okMetas = append(okMetas, m)
	}
	return out, okMetas, nil
}

// resume seeds state from the server's view of the user once, when no local state was built or acks
// were reset (new login). Append rows are only seeded when the server's tail matches the local file;
// otherwise they are resent from 0 and the server dedupes or answers conflict.
func (e *Engine) resume(ctx context.Context) error {
	if _, ok, err := e.State.Setting(ctx, "resumed"); err != nil || ok {
		return err
	}
	remote, err := e.Up.Files(ctx)
	if err != nil {
		return err
	}
	pick := map[[2]string]protocol.RemoteFile{}
	for _, r := range remote {
		k := [2]string{r.Path, r.HeadHash}
		if old, ok := pick[k]; !ok || (r.Scope == protocol.ScopeDevice && old.Scope != protocol.ScopeDevice) {
			pick[k] = r // a device-scoped entry wins over the per-user one
		}
	}
	for _, r := range pick {
		row, ok, err := e.State.Get(ctx, r.Path, r.HeadHash)
		if err != nil {
			return err
		}
		if ok && (row.AckedOffset != 0 || row.ContentHash != "") {
			continue
		}
		if !ok {
			row = state.File{RelPath: r.Path, HeadHash: r.HeadHash, Kind: r.Kind}
		}
		if r.Kind == protocol.KindAppend && !e.tailMatches(r, row.SpoolPath) {
			continue
		}
		row.AckedOffset, row.ContentHash, row.DeviceScoped = r.EndOffset, r.ContentHash, r.Scope == protocol.ScopeDevice
		if err := e.State.Put(ctx, row); err != nil {
			return err
		}
	}
	return e.State.SetSetting(ctx, "resumed", "1")
}

// tailMatches reports whether the local file (original, else spool) holds the server's last line at
// its offset and ends where the server's file ends.
func (e *Engine) tailMatches(r protocol.RemoteFile, spoolPath string) bool {
	if r.EndOffset == 0 {
		return true
	}
	for _, p := range []string{filepath.Join(e.Root, filepath.FromSlash(r.Path)), spoolPath} {
		if p == "" {
			continue
		}
		lines, next, err := reader.ReadLines(p, r.TailOffset, 1)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		return err == nil && len(lines) > 0 && protocol.HashHex(lines[0]) == r.TailHash && next == r.EndOffset
	}
	return false
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
				e.fileErrs = append(e.fileErrs, err) // unreadable: skip this file only
				continue
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
	// A row whose original is gone still owns the live spool path (one entry per rel path). A new file
	// with another head under the same rel would overwrite it, so move the old entry aside first.
	for _, it := range items {
		r := &it.row
		if it.abs != "" || r.Kind != protocol.KindAppend || r.FileID == "" || r.SpoolPath != e.Spool.Path(r.RelPath) {
			continue
		}
		old, err := e.Spool.Retire(r.RelPath, r.FileID)
		if err != nil {
			return nil, err
		}
		if old != "" {
			r.SpoolPath = old
			if err := e.State.Put(ctx, *r); err != nil {
				return nil, err
			}
		}
	}
	live := items[:0]
	for _, it := range items {
		orig := it.row
		if err := e.prepare(ctx, it); err != nil {
			e.fileErrs = append(e.fileErrs, err)
			continue
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
		return e.spoolSnapshot(it)
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
		if fileExists(r.SpoolPath) {
			if lines, _, err := reader.ReadLines(r.SpoolPath, r.AckedOffset, 1); err != nil || len(lines) == 0 {
				return nil //nolint:nilerr // nothing unacked in the old entry: Ensure replaces it
			}
		}
		// A missing live entry may be a crash after the rename: Retire then returns the .old path.
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

// spoolSnapshot spools a changed snapshot during discovery, so a long outage cannot lose any of them.
func (e *Engine) spoolSnapshot(it *item) error {
	if it.abs == "" {
		return nil
	}
	_, hash, err := reader.Snapshot(it.abs)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if hash == it.row.ContentHash {
		return nil
	}
	return e.ensureSpool(it)
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

// skip records an entry above MaxLine, which is never uploaded; its spool copy then stays for good.
func (e *Engine) skip(ctx context.Context, it *item, off int64, raw []byte, length int64) error {
	return e.State.AddSkipped(ctx, state.Skipped{RelPath: it.row.RelPath, HeadHash: it.row.HeadHash, Offset: off, Length: length, SHA256: protocol.HashHex(raw)})
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// drop removes the spool entry and the row of an item that is done and has no original.
func (e *Engine) drop(ctx context.Context, it *item) error {
	if keep, err := e.State.HasSkipped(ctx, it.row.RelPath, it.row.HeadHash); err != nil || keep {
		return err // the spool copy holds an entry the server never took
	}
	if it.row.SpoolPath != "" {
		if err := e.Spool.Remove(it.row.SpoolPath); err != nil {
			return err
		}
	}
	return e.State.Delete(ctx, it.row.RelPath, it.row.HeadHash)
}

// finish handles a fully acked item: gone → forget it; present → release the spool entry.
func (e *Engine) finish(ctx context.Context, it *item) error {
	keep, err := e.State.HasSkipped(ctx, it.row.RelPath, it.row.HeadHash)
	if err != nil {
		return err
	}
	if it.drain { // old file drained: switch the same row over to the new file
		if !keep {
			if err := e.Spool.Remove(it.row.SpoolPath); err != nil {
				return err
			}
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
	if it.row.SpoolPath == "" || keep {
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
				it.done = true
				e.fileErrs = append(e.fileErrs, err)
				continue
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
			if len(content) > e.MaxLine {
				if it.abs != "" && !it.spooled {
					if err := e.ensureSpool(it); err != nil {
						return req, nil, err
					}
				}
				if err := e.skip(ctx, it, 0, content, int64(len(content))); err != nil {
					return req, nil, err
				}
				it.row.ContentHash, it.row.SizeSeen, it.done = hash, 0, true // treated as handled until it changes again
				if err := e.State.Put(ctx, it.row); err != nil {
					return req, nil, err
				}
				continue
			}
			big := len(content) > protocol.ChunkSize
			if len(metas) > 0 && (big || len(content) > rem) {
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
			var ref *partRef
			if big {
				key := partKey{r.RelPath, "", 0, hash}
				var idx int
				c, idx = e.nextPart(key, content)
				ref = &partRef{key, idx}
			}
			req.Files = append(req.Files, protocol.FileBatch{Kind: protocol.KindSnapshot, Path: r.RelPath, Content: &c})
			metas = append(metas, sent{it: it, hash: hash, part: ref})
			if big {
				return req, metas, nil // a part goes in a request of its own
			}
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
			it.done = true
			e.fileErrs = append(e.fileErrs, err)
			continue
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
		if i := slices.IndexFunc(lines, func(l []byte) bool { return len(l) > protocol.ChunkSize }); i == 0 && len(lines[0]) <= e.MaxLine {
			if len(metas) > 0 {
				break // a part goes in a request of its own
			}
			key := partKey{r.RelPath, r.HeadHash, r.AckedOffset, protocol.HashHex(lines[0])}
			chunk, idx := e.nextPart(key, lines[0])
			fb := protocol.FileBatch{Kind: protocol.KindAppend, Path: r.RelPath, HeadHash: r.HeadHash, FromOffset: r.AckedOffset, Lines: []protocol.Line{chunk}}
			if r.DeviceScoped {
				fb.Scope = protocol.ScopeDevice
			}
			gap, err := e.State.HasSkipped(ctx, r.RelPath, r.HeadHash)
			if err != nil {
				return req, nil, err
			}
			fb.AllowGap = gap
			req.Files = append(req.Files, fb)
			return req, []sent{{it: it, from: r.AckedOffset, part: &partRef{key, idx}}}, nil
		} else if i == 0 {
			// Advance past the line so the rest of the file syncs; the gap stays visible in `braid status`.
			n := int64(len(lines[0])) + 1
			if err := e.skip(ctx, it, r.AckedOffset, lines[0], n); err != nil {
				return req, nil, err
			}
			it.row.AckedOffset += n
			if err := e.State.Put(ctx, it.row); err != nil {
				return req, nil, err
			}
			goto retry
		} else if i > 0 {
			lines = lines[:i]
			next = protocol.EndOffset(r.AckedOffset, lines)
		}
		if len(metas) > 0 && next-r.AckedOffset > int64(rem) {
			break // oversized first line goes alone
		}
		if k := e.MaxBatchLines - nlines; len(lines) > k {
			lines = lines[:k]
			next = protocol.EndOffset(r.AckedOffset, lines)
		}
		fb := protocol.FileBatch{Kind: protocol.KindAppend, Path: r.RelPath, HeadHash: r.HeadHash, FromOffset: r.AckedOffset}
		if r.DeviceScoped {
			fb.Scope = protocol.ScopeDevice
		}
		gap, err := e.State.HasSkipped(ctx, r.RelPath, r.HeadHash)
		if err != nil {
			return req, nil, err
		}
		fb.AllowGap = gap
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
		case protocol.StatusPending:
			if m.part == nil {
				return uploaded, errors.New("syncer: server answered pending to a whole line")
			}
			if ps := e.parts[m.part.key]; ps != nil {
				if m.part.idx+1 < len(ps.chunks) {
					ps.next = m.part.idx + 1
				} else { // all chunks sent yet not assembled (server lost some): start over next pass
					delete(e.parts, m.part.key)
					m.it.done = true
				}
			}
			continue
		case protocol.StatusOK:
			if m.part != nil {
				delete(e.parts, m.part.key)
			}
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
		case protocol.StatusConflict:
			if r.DeviceScoped {
				return uploaded, fmt.Errorf("syncer: server reported conflict for device-scoped %s", r.RelPath)
			}
			r.DeviceScoped, r.AckedOffset = true, 0 // resend from 0 under the device-scoped identity
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
