// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package httpx

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	idleEvict     = 10 * time.Minute
	sweepInterval = time.Minute
)

type bucket struct {
	tokens float64
	last   time.Time
}

// Limiter is a set of token buckets keyed by string; a fresh key starts full.
type Limiter struct {
	mu        sync.Mutex
	cap       float64
	perSec    float64
	now       func() time.Time
	buckets   map[string]*bucket
	lastSweep time.Time
}

// NewLimiter allows perMinute requests per minute per key, with burst perMinute.
func NewLimiter(perMinute int, now func() time.Time) *Limiter {
	return &Limiter{cap: float64(perMinute), perSec: float64(perMinute) / 60, now: now,
		buckets: map[string]*bucket{}, lastSweep: now()}
}

// Allow consumes one token for key if available.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := l.now()
	if t.Sub(l.lastSweep) >= sweepInterval {
		for k, b := range l.buckets {
			if t.Sub(b.last) > idleEvict {
				delete(l.buckets, k)
			}
		}
		l.lastSweep = t
	}
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.cap, last: t}
		l.buckets[key] = b
	}
	if d := t.Sub(b.last).Seconds(); d > 0 {
		b.tokens = math.Min(l.cap, b.tokens+d*l.perSec)
	}
	b.last = t
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// TooMany writes 429 with Retry-After (whole seconds, at least 1).
func TooMany(w http.ResponseWriter, retryAfter time.Duration) {
	secs := int(math.Ceil(retryAfter.Seconds()))
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	WriteJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate_limited"})
}
