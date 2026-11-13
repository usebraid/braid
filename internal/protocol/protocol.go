// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package protocol holds the wire types and pure helpers shared by daemon and server.
package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Version is the wire protocol version.
const Version = 1

// HeaderVersion is the HTTP header carrying the protocol version.
const HeaderVersion = "Braid-Protocol"

// Body and path size limits.
const (
	MaxCompressedBody   = 8 << 20
	MaxDecompressedBody = 64 << 20
	MaxPathLen          = 1024
	MaxLinesPerRequest  = 20000
	MaxFilesPerRequest  = 5000 // the daemon never sends more entries than lines per batch
)

// Chunking of lines and snapshots too large for one request. ChunkSize raw bytes escape to at most
// six times as many JSON bytes, so a request holding one chunk stays below MaxCompressedBody
// (and MaxDecompressedBody) even uncompressed and at worst-case escaping.
const (
	ChunkSize = MaxCompressedBody / 8
	// MaxLineBytes caps one line or snapshot: derive, the viewer and Postgres assemble it in memory.
	// Larger ones are never uploaded (the daemon records them as skipped).
	MaxLineBytes = 256 << 20
	maxParts     = MaxLineBytes/ChunkSize + 1
)

// Kind is the sync mode of a file: append-only log or whole-file snapshot.
type Kind string

// Known file kinds.
const (
	KindAppend   Kind = "append"
	KindSnapshot Kind = "snapshot"
)

// ErrTooManyLines means a request carries more than MaxLinesPerRequest lines in total.
var ErrTooManyLines = errors.New("protocol: too many lines in request")

// ErrTooManyFiles means a request carries more than MaxFilesPerRequest file entries.
var ErrTooManyFiles = errors.New("protocol: too many files in request")

// ErrBadLine means a Line has both or neither of Text and B64 set.
var ErrBadLine = errors.New("protocol: line must set exactly one of text or b64")

// Part marks a Line as one chunk of a longer line (or snapshot): chunk Index of Count, of a
// line of TotalLen bytes with the given hex SHA-256. A request carries one part entry alone.
type Part struct {
	Index    int    `json:"index"`
	Count    int    `json:"count"`
	TotalLen int64  `json:"total_len"`
	SHA256   string `json:"sha256"`
}

// Line carries raw bytes: Text when valid UTF-8, otherwise B64. With Part set, the bytes are one chunk.
type Line struct {
	Text *string `json:"text,omitempty"`
	B64  []byte  `json:"b64,omitempty"`
	Part *Part   `json:"part,omitempty"`
}

// SplitLine cuts raw into chunks of at most size bytes, on rune boundaries when raw is valid UTF-8.
func SplitLine(raw []byte, size int) []Line {
	valid := utf8.Valid(raw)
	var cuts [][]byte
	for len(raw) > 0 {
		n := min(size, len(raw))
		for valid && n < len(raw) && !utf8.RuneStart(raw[n]) {
			n--
		}
		cuts = append(cuts, raw[:n])
		raw = raw[n:]
	}
	sum := HashHex(bytes.Join(cuts, nil))
	total := int64(0)
	for _, c := range cuts {
		total += int64(len(c))
	}
	out := make([]Line, len(cuts))
	for i, c := range cuts {
		out[i] = EncodeLine(c)
		out[i].Part = &Part{Index: i, Count: len(cuts), TotalLen: total, SHA256: sum}
	}
	return out
}

func (p Part) valid() bool {
	return p.Count >= 2 && p.Count <= maxParts && p.Index >= 0 && p.Index < p.Count &&
		p.TotalLen > ChunkSize && p.TotalLen <= MaxLineBytes && validHash(p.SHA256)
}

// EncodeLine wraps raw bytes as Text when valid UTF-8, otherwise as B64.
func EncodeLine(raw []byte) Line {
	if utf8.Valid(raw) {
		s := string(raw)
		return Line{Text: &s}
	}
	return Line{B64: raw}
}

// Bytes returns the raw bytes, or ErrBadLine if both or neither field is set.
func (l Line) Bytes() ([]byte, error) {
	if (l.Text != nil) == (l.B64 != nil) {
		return nil, ErrBadLine
	}
	if l.Text != nil {
		return []byte(*l.Text), nil
	}
	return l.B64, nil
}

// Scope selects the identity of an append file: "" = per user, ScopeDevice = per (user, device).
type Scope string

// ScopeDevice is the fallback after a conflict (01 section 3.4).
const ScopeDevice Scope = "device"

// FileBatch is one file's update inside an ingest request.
type FileBatch struct {
	Kind       Kind   `json:"kind"`
	Path       string `json:"path"`
	HeadHash   string `json:"head_hash"`
	FromOffset int64  `json:"from_offset"`
	Lines      []Line `json:"lines,omitempty"`
	Content    *Line  `json:"content,omitempty"`
	Scope      Scope  `json:"scope,omitempty"`
	// AllowGap lets from_offset exceed the server's end: the daemon skipped a line or snapshot
	// the protocol can never carry (see MaxLineJSON) and keeps it locally.
	AllowGap bool `json:"allow_gap,omitempty"`
}

// IngestRequest is the body of POST /v1/ingest.
type IngestRequest struct {
	Files []FileBatch `json:"files"`
}

// FileStatus is the server's verdict for one file.
type FileStatus string

// Known file statuses.
const (
	StatusOK       FileStatus = "ok"
	StatusGap      FileStatus = "gap"
	StatusConflict FileStatus = "conflict"
	StatusPending  FileStatus = "pending" // a part was stored; the line is not complete yet
)

// FileResult reports the server state of one ingested file.
type FileResult struct {
	Path      string     `json:"path"`
	HeadHash  string     `json:"head_hash"`
	Status    FileStatus `json:"status"`
	EndOffset int64      `json:"end_offset"`
}

// IngestResponse is the response of POST /v1/ingest.
type IngestResponse struct {
	Files []FileResult `json:"files"`
}

// RemoteFile is one entry of GET /v1/files.
type RemoteFile struct {
	Path        string `json:"path"`
	HeadHash    string `json:"head_hash"`
	Kind        Kind   `json:"kind"`
	EndOffset   int64  `json:"end_offset"`
	ContentHash string `json:"content_hash,omitempty"` // hex sha256 of latest snapshot
	Scope       Scope  `json:"scope,omitempty"`
	TailOffset  int64  `json:"tail_offset,omitempty"` // append: offset of the stored line with the highest offset
	TailHash    string `json:"tail_hash,omitempty"`   // hex sha256 of that line
}

// Page sizes of GET /v1/files.
const (
	DefaultFilesLimit = 500
	MaxFilesLimit     = 1000
)

// FilesPage is the response of GET /v1/files?after=<cursor>&limit=<n>; Next is the cursor of the next page, empty on the last.
type FilesPage struct {
	Files []RemoteFile `json:"files"`
	Next  string       `json:"next,omitempty"`
}

// ErrorResponse is the JSON error body; Server and Min report version mismatches.
type ErrorResponse struct {
	Error  string `json:"error"`
	Server int    `json:"server,omitempty"`
	Min    int    `json:"min,omitempty"`
}

// EndOffset returns from + sum(len(line)+1).
func EndOffset(from int64, lines [][]byte) int64 {
	for _, l := range lines {
		from += int64(len(l)) + 1
	}
	return from
}

// HashHex returns the lowercase hex SHA-256 of b.
func HashHex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// ValidPath accepts relative, '/'-separated paths whose extension matches k.
func ValidPath(p string, k Kind) error {
	if p == "" || len(p) > MaxPathLen {
		return errors.New("protocol: bad path length")
	}
	if strings.ContainsFunc(p, unicode.IsControl) || strings.ContainsAny(p, `\`) {
		return errors.New("protocol: path contains forbidden character")
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return errors.New("protocol: bad path segment")
		}
	}
	switch k {
	case KindAppend:
		if !strings.HasSuffix(p, ".jsonl") {
			return errors.New("protocol: append path must end in .jsonl")
		}
	case KindSnapshot:
		if !strings.HasSuffix(p, ".json") {
			return errors.New("protocol: snapshot path must end in .json")
		}
	default:
		return fmt.Errorf("protocol: unknown kind %q", k)
	}
	return nil
}

func validHash(h string) bool {
	if len(h) != 64 {
		return false
	}
	for _, c := range h {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// Validate checks a whole request. An append entry with zero lines is valid.
func (r IngestRequest) Validate() error {
	if len(r.Files) > MaxFilesPerRequest {
		return ErrTooManyFiles
	}
	total := 0
	for _, f := range r.Files {
		total += len(f.Lines)
	}
	if total > MaxLinesPerRequest {
		return ErrTooManyLines
	}
	for i, f := range r.Files {
		if err := ValidPath(f.Path, f.Kind); err != nil {
			return fmt.Errorf("files[%d]: %w", i, err)
		}
		if f.Scope != "" && (f.Scope != ScopeDevice || f.Kind != KindAppend) {
			return fmt.Errorf("files[%d]: bad scope", i)
		}
		switch f.Kind {
		case KindAppend:
			if !validHash(f.HeadHash) {
				return fmt.Errorf("files[%d]: bad head_hash", i)
			}
			if f.FromOffset < 0 {
				return fmt.Errorf("files[%d]: negative from_offset", i)
			}
			if f.Content != nil {
				return fmt.Errorf("files[%d]: append must not carry content", i)
			}
			for j, l := range f.Lines {
				b, err := l.Bytes()
				if err != nil {
					return fmt.Errorf("files[%d].lines[%d]: %w", i, j, err)
				}
				if l.Part != nil {
					if len(f.Lines) != 1 || !l.Part.valid() || len(b) > ChunkSize || bytes.IndexByte(b, '\n') >= 0 {
						return fmt.Errorf("files[%d].lines[%d]: bad part", i, j)
					}
					continue
				}
				if bytes.IndexByte(b, '\n') >= 0 { // EndOffset assumes one '\n' per line
					return fmt.Errorf("files[%d].lines[%d]: embedded newline", i, j)
				}
			}
		case KindSnapshot:
			if f.HeadHash != "" || len(f.Lines) != 0 || f.Content == nil {
				return fmt.Errorf("files[%d]: snapshot needs content only", i)
			}
			b, err := f.Content.Bytes()
			if err != nil {
				return fmt.Errorf("files[%d].content: %w", i, err)
			}
			if p := f.Content.Part; p != nil && (!p.valid() || len(b) > ChunkSize) {
				return fmt.Errorf("files[%d].content: bad part", i)
			}
		}
	}
	return nil
}
