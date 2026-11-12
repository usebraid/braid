// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package client

import (
	"math/rand/v2"
	"time"
)

// Backoff computes jittered exponential delays.
type Backoff struct {
	Min, Max time.Duration
	Rand     func() float64 // [0,1); defaults to math/rand/v2
}

// Next returns min(Max, Min*2^attempt) scaled by a jitter factor in [0.5, 1.0).
func (b Backoff) Next(attempt int) time.Duration {
	r := b.Rand
	if r == nil {
		r = rand.Float64 //nolint:gosec // jitter, not security
	}
	d := b.Max
	if attempt < 0 {
		attempt = 0
	}
	if attempt < 62 && b.Min <= b.Max>>uint(attempt) {
		d = b.Min << uint(attempt)
	}
	return time.Duration(float64(d) * (0.5 + r()/2))
}
