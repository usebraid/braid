// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package cli_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/usebraid/braid/internal/daemon/cli"
	"github.com/usebraid/braid/internal/daemon/client"
	"github.com/usebraid/braid/internal/daemon/syncer"
)

func TestLock(t *testing.T) {
	dir := t.TempDir()
	rel, err := cli.AcquireLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cli.AcquireLock(dir); !errors.Is(err, cli.ErrLocked) {
		t.Fatalf("second lock: %v", err)
	}
	rel()
	if _, err := cli.AcquireLock(dir); err != nil {
		t.Fatalf("after release: %v", err)
	}
}

func TestRunnerRetriesWithBackoff(t *testing.T) {
	trig := make(chan struct{}, 1)
	trig <- struct{}{}
	var slept []time.Duration
	calls := 0
	ctx, cancel := context.WithCancel(context.Background())
	r := cli.NewTestRunner(func(context.Context) (syncer.Result, error) {
		calls++
		if calls < 3 {
			return syncer.Result{}, &client.RetryableError{Status: 503}
		}
		cancel()
		return syncer.Result{}, nil
	}, trig, func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil })
	if err := r.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if calls != 3 || len(slept) != 2 || slept[1] <= slept[0]/2 {
		t.Fatalf("calls %d slept %v", calls, slept)
	}
}

func TestRunnerUnauthorizedKeepsRunning(t *testing.T) {
	trig := make(chan struct{}, 2)
	trig <- struct{}{}
	trig <- struct{}{}
	calls := 0
	ctx, cancel := context.WithCancel(context.Background())
	r := cli.NewTestRunner(func(context.Context) (syncer.Result, error) {
		calls++
		if calls == 2 {
			cancel()
		}
		return syncer.Result{}, client.ErrUnauthorized
	}, trig, func(context.Context, time.Duration) error { return nil })
	if err := r.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls %d", calls)
	}
}
