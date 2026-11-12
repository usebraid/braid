// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package spool keeps not-yet-acked transcript data alive after Claude Code deletes the original.
package spool

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/usebraid/braid/internal/daemon/reader"
)

// Spool mirrors files below Dir via hardlinks, falling back to copies.
type Spool struct {
	Dir  string
	link func(oldname, newname string) error
}

// New returns a Spool rooted at dir.
func New(dir string) *Spool { return &Spool{Dir: dir, link: os.Link} }

// Path returns the spool path for relPath ("/" separators).
func (s *Spool) Path(relPath string) string {
	return filepath.Join(s.Dir, filepath.FromSlash(relPath))
}

func validRel(rel string) error {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "\\") || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return fmt.Errorf("spool: invalid relative path %q", rel)
	}
	for _, seg := range strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return fmt.Errorf("spool: invalid relative path %q", rel)
		}
	}
	return nil
}

// Ensure makes sure a spool entry for relPath mirrors abs. Returns the spool path and whether it is a copy.
func (s *Spool) Ensure(relPath, abs, fileID string) (string, bool, error) {
	if err := validRel(relPath); err != nil {
		return "", false, err
	}
	p := s.Path(relPath)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", false, err
	}
	if id, err := reader.FileID(p); err == nil {
		if id == fileID {
			return p, false, nil
		}
		// Different file: either a copy (refresh when the original grew) or a stale entry to replace.
		if copied, err := s.refreshCopy(p, abs); copied || err != nil {
			return p, copied, err
		}
		if err := os.Remove(p); err != nil {
			return "", false, err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", false, err
	}
	if err := s.link(abs, p); err == nil {
		return p, false, nil
	}
	return p, true, copyFile(abs, p)
}

// refreshCopy re-copies when linking is not possible and the original is larger than the entry.
func (s *Spool) refreshCopy(p, abs string) (bool, error) {
	if err := s.link(abs, p+".probe"); err == nil {
		_ = os.Remove(p + ".probe")
		return false, nil // linkable: caller replaces the stale entry
	}
	src, err := os.Stat(abs)
	if err != nil {
		return false, err
	}
	dst, err := os.Stat(p)
	if err != nil {
		return false, err
	}
	if src.Size() <= dst.Size() {
		return true, nil
	}
	return true, copyFile(abs, p)
}

func copyFile(src, dst string) (err error) {
	in, err := reader.Open(src)
	if err != nil {
		return err
	}
	defer in.Close() //nolint:errcheck // read-only handle
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".spool-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst) // CreateTemp creates 0600
}

// Retire moves the current entry aside to <path>.<fileID>.old; a missing entry returns "".
func (s *Spool) Retire(relPath, fileID string) (string, error) {
	if err := validRel(relPath); err != nil {
		return "", err
	}
	p := s.Path(relPath)
	old := p + "." + strings.ReplaceAll(fileID, ":", "-") + ".old"
	if err := os.Rename(p, old); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return old, nil
}

// Remove deletes a spool path (missing is fine) and prunes empty parent dirs below Dir.
func (s *Spool) Remove(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	root := filepath.Clean(s.Dir)
	for d := filepath.Dir(filepath.Clean(path)); strings.HasPrefix(d, root+string(filepath.Separator)); d = filepath.Dir(d) {
		if os.Remove(d) != nil { // non-empty or gone: stop
			break
		}
	}
	return nil
}
