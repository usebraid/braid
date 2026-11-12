//go:build !windows

// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package reader

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func open(path string) (*os.File, error) { return os.Open(path) } //nolint:gosec // daemon-chosen transcript path

func fileID(f *os.File) (string, error) {
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errors.New("reader: no stat_t")
	}
	return fmt.Sprintf("%d:%d", st.Dev, st.Ino), nil //nolint:unconvert // field widths differ per OS
}
