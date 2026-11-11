// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package protocol holds the wire types and pure helpers shared by daemon and server.
package protocol

import (
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
)

// Kind is the sync mode of a file: append-only log or whole-file snapshot.
type Kind string

// Known file kinds.
const (
	KindAppend   Kind = "append"
	KindSnapshot Kind = "snapshot"
)

// ErrBadLine means a Line has both or neither of Text and B64 set.
var ErrBadLine = errors.New("protocol: line must set exactly one of text or b64")

// Line carries raw bytes: Text when valid UTF-8, otherwise B64.
type Line struct {
	Text *string `json:"text,omitempty"`
	B64  []byte  `json:"b64,omitempty"`
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

// FileBatch is one file's update inside an ingest request.
type FileBatch struct {
	Kind       Kind   `json:"kind"`
	Path       string `json:"path"`
	HeadHash   string `json:"head_hash"`
	FromOffset int64  `json:"from_offset"`
	Lines      []Line `json:"lines,omitempty"`
	Content    *Line  `json:"content,omitempty"`
}

// IngestRequest is the body of POST /v1/ingest.
type IngestRequest struct {
	Files []FileBatch `json:"files"`
}

// FileStatus is the server's verdict for one file.
type FileStatus string

// Known file statuses.
const (
	StatusOK  FileStatus = "ok"
	StatusGap FileStatus = "gap"
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
	for i, f := range r.Files {
		if err := ValidPath(f.Path, f.Kind); err != nil {
			return fmt.Errorf("files[%d]: %w", i, err)
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
				if _, err := l.Bytes(); err != nil {
					return fmt.Errorf("files[%d].lines[%d]: %w", i, j, err)
				}
			}
		case KindSnapshot:
			if f.HeadHash != "" || len(f.Lines) != 0 || f.Content == nil {
				return fmt.Errorf("files[%d]: snapshot needs content only", i)
			}
			if _, err := f.Content.Bytes(); err != nil {
				return fmt.Errorf("files[%d].content: %w", i, err)
			}
		}
	}
	return nil
}
