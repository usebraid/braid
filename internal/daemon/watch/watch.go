// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package watch turns filesystem events and periodic rescans into coalesced triggers.
package watch

import (
	"context"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Triggers emits (coalesced, never blocking the watcher) whenever something below root changes,
// every rescan interval, and once at start. New subdirectories are watched as they appear.
// Missing root: retries adding the watch at every rescan. Closes the channel when ctx ends.
func Triggers(ctx context.Context, root string, rescan time.Duration) (<-chan struct{}, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	out := make(chan struct{}, 1)
	fire := func() {
		select {
		case out <- struct{}{}:
		default:
		}
	}
	// addTree watches dir and every directory below it; Add on a watched dir is a no-op.
	addTree := func(dir string) {
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				_ = w.Add(p) // vanished or unreadable dirs are picked up at the next rescan
			}
			return nil
		})
	}
	addTree(root)
	fire()
	go func() {
		defer close(out)
		defer func() { _ = w.Close() }()
		tick := time.NewTicker(rescan)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				addTree(root)
				fire()
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if ev.Has(fsnotify.Create) {
					addTree(ev.Name) // no-op for plain files; covers dirs created with content
				}
				fire()
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
				fire() // events may have been lost (overflow): rescan
			}
		}
	}()
	return out, nil
}
