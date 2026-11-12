// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package reader reads transcript files that Claude Code appends to concurrently.
package reader

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"

	"github.com/usebraid/braid/internal/protocol"
)

// Open opens for reading; on Windows with FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE.
func Open(path string) (*os.File, error) { return open(path) }

// HeadHash returns hex sha256 of the first complete line (without '\n'); ok=false if no complete line yet.
func HeadHash(path string) (hash string, ok bool, err error) {
	lines, _, err := ReadLines(path, 0, 1)
	if err != nil || len(lines) == 0 {
		return "", false, err
	}
	return protocol.HashHex(lines[0]), true, nil
}

// ReadLines returns complete lines starting at from, stopping before maxBytes (line bytes plus
// their '\n') would be exceeded, but always returning the first line if one is complete.
// next = from + Σ(len+1). A trailing partial line is never returned.
func ReadLines(path string, from int64, maxBytes int) (lines [][]byte, next int64, err error) {
	f, err := Open(path)
	if err != nil {
		return nil, from, err
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return nil, from, err
	}
	next = from
	if from >= fi.Size() {
		return nil, next, nil
	}
	// Only the size seen now is read, so bytes appended meanwhile are left for the next call.
	br := bufio.NewReaderSize(io.NewSectionReader(f, from, fi.Size()-from), 64<<10)
	used := 0
	for {
		line, rerr := br.ReadBytes('\n')
		if rerr != nil {
			if errors.Is(rerr, io.EOF) { // partial tail (or nothing): discard
				return lines, next, nil
			}
			return lines, next, rerr
		}
		if len(lines) > 0 && used+len(line) > maxBytes {
			return lines, next, nil
		}
		used += len(line)
		next += int64(len(line))
		lines = append(lines, bytes.TrimSuffix(line, []byte{'\n'}))
	}
}

// Snapshot returns the whole content and its hex sha256.
func Snapshot(path string) (content []byte, hash string, err error) {
	f, err := Open(path)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = f.Close() }()
	content, err = io.ReadAll(f)
	if err != nil {
		return nil, "", err
	}
	return content, protocol.HashHex(content), nil
}

// FileID returns a stable identity ("dev:ino" on unix, volume serial + file index on Windows).
func FileID(path string) (string, error) {
	f, err := Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	return fileID(f)
}
