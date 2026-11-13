//go:build windows

// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package reader

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func open(path string) (*os.File, error) {
	// CreateFile does not add the \\?\ prefix that os.Open adds, so paths over 260 characters would fail.
	// shortcut: UNC paths (\\server\share) are passed as is; add \\?\UNC\ if they are ever spooled.
	if abs, err := filepath.Abs(path); err == nil && len(abs) >= 248 && len(abs) > 2 && abs[1] == ':' {
		path = `\\?\` + abs
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

func fileID(f *os.File) (string, error) {
	var i syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &i); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x:%x%08x", i.VolumeSerialNumber, i.FileIndexHigh, i.FileIndexLow), nil
}
