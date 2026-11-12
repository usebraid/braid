// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package cli

import (
	"context"
	"time"

	"github.com/usebraid/braid/internal/daemon/client"
	"github.com/usebraid/braid/internal/daemon/syncer"
)

// NewTestRunner builds a Runner around a fake pass function.
func NewTestRunner(pass func(context.Context) (syncer.Result, error), trig <-chan struct{}, sleep func(context.Context, time.Duration) error) *Runner {
	return &Runner{pass: pass, Trigger: trig, Sleep: sleep, Backoff: client.Backoff{Min: time.Second, Max: 5 * time.Minute}}
}
