// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package cli holds the daemon's command implementations.
package cli

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"

	"github.com/usebraid/braid/internal/daemon/client"
	"github.com/usebraid/braid/internal/daemon/syncer"
)

// ErrLocked is returned by AcquireLock when another process holds the lock.
var ErrLocked = errors.New("another braid process is syncing")

// AcquireLock takes <data>/braid.lock non-blocking; returns ErrLocked if held.
func AcquireLock(dataDir string) (release func(), err error) {
	l := flock.New(filepath.Join(dataDir, "braid.lock"))
	ok, err := l.TryLock()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrLocked
	}
	return func() { _ = l.Unlock() }, nil
}

// Runner drives sync passes from triggers.
type Runner struct {
	Engine  *syncer.Engine
	Trigger <-chan struct{}
	Backoff client.Backoff
	Log     *slog.Logger
	Sleep   func(ctx context.Context, d time.Duration) error // default: timer with ctx

	pass func(context.Context) (syncer.Result, error) // test hook; default Engine.Pass
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Run loops: on trigger it runs a pass, retrying retryable errors with backoff.
// Unauthorized, protocol and permanent errors are logged; the loop keeps waiting
// for triggers (the spool keeps the data). Returns nil when ctx ends.
func (r *Runner) Run(ctx context.Context) error {
	pass, sleep, log := r.pass, r.Sleep, r.Log
	if pass == nil {
		pass = r.Engine.Pass
	}
	if sleep == nil {
		sleep = sleepCtx
	}
	if log == nil {
		log = slog.Default()
	}
	unauthLogged := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case _, ok := <-r.Trigger:
			if !ok {
				return nil
			}
		}
		for attempt := 0; ctx.Err() == nil; attempt++ {
			res, err := pass(ctx)
			var re *client.RetryableError
			var pm *client.ProtocolMismatchError
			switch {
			case err == nil:
				unauthLogged = false
				log.Debug("sync pass done", "uploaded", res.Uploaded, "pending_files", res.PendingFiles)
			case errors.As(err, &re):
				d := r.Backoff.Next(attempt)
				if re.RetryAfter > d {
					d = re.RetryAfter
				}
				log.Warn("sync failed, retrying", "err", err, "in", d)
				if sleep(ctx, d) != nil {
					return nil
				}
				continue
			case errors.Is(err, client.ErrUnauthorized):
				if !unauthLogged {
					log.Error("token rejected, run `braid login`")
					unauthLogged = true
				}
			case errors.As(err, &pm):
				log.Error("protocol mismatch", "err", err)
			default: // permanent errors and partial-scan warnings after a successful pass
				log.Warn("sync pass reported an error", "err", err)
			}
			break
		}
	}
}
