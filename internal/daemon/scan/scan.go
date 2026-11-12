// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package scan finds transcript files to sync below the Claude projects root.
package scan

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/usebraid/braid/internal/protocol"
)

// Entry is a classified regular file.
type Entry struct {
	RelPath, AbsPath string
	Kind             protocol.Kind
}

// Classify maps a '/'-separated relative path to its sync kind.
func Classify(relPath string) (protocol.Kind, bool) {
	for _, seg := range strings.Split(relPath, "/") {
		if strings.HasPrefix(seg, ".") || seg == "memory" {
			return "", false
		}
	}
	switch {
	case strings.HasSuffix(relPath, ".jsonl"):
		return protocol.KindAppend, true
	case strings.HasSuffix(relPath, ".json"):
		return protocol.KindSnapshot, true
	}
	return "", false
}

// Walk returns all classified regular files below root, sorted by RelPath.
// A missing root yields no entries and no error. A symlinked root is resolved;
// symlinks below it are skipped. Unreadable entries are skipped: the partial
// result is returned together with a joined non-nil error naming them.
func Walk(root string) ([]Entry, error) {
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Entry
	var skipped []error
	err = filepath.WalkDir(base, func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			if p == base {
				return err
			}
			if !os.IsNotExist(err) { // vanished mid-walk is not an error
				skipped = append(skipped, err)
			}
			return nil // for a failed dir, WalkDir does not descend
		}
		if !de.Type().IsRegular() {
			return nil // dirs are descended; symlinks and specials skipped
		}
		rel, err := filepath.Rel(base, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if k, ok := Classify(rel); ok {
			out = append(out, Entry{RelPath: rel, AbsPath: p, Kind: k})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RelPath < out[j].RelPath })
	return out, errors.Join(skipped...)
}
