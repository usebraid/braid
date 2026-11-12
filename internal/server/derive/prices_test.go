// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package derive

import (
	"math"
	"testing"

	"github.com/usebraid/braid/internal/server/derive/parse"
)

func TestCost(t *testing.T) {
	cases := []struct {
		name      string
		model     string
		u         parse.Usage
		want      float64
		estimated bool
		unknown   bool
	}{
		{"opus 5.5 all classes", "claude-opus-5-5", parse.Usage{Input: 1000, Output: 2000, CacheRead: 3000, CacheWrite5m: 4000, CacheWrite1h: 5000}, 0.1046, false, false},
		{"dated id", "claude-haiku-4-5-20251001", parse.Usage{Input: 1_000_000}, 1.0, false, false},
		{"context suffix", "claude-opus-5-5[1m]", parse.Usage{Input: 1_000_000}, 4.0, false, false},
		{"haiku 5.5 long prompt tier", "claude-haiku-5-5", parse.Usage{Input: 60000, CacheRead: 50000, Output: 1000}, 0.035, false, false},
		{"haiku 5.5 short prompt", "claude-haiku-5-5", parse.Usage{Input: 60000, CacheRead: 30000, Output: 1000}, 0.0068, false, false},
		{"web search", "claude-sonnet-5-5", parse.Usage{WebSearch: 3}, 0.03, false, false},
		{"thinking is not added", "claude-opus-5-5", parse.Usage{Output: 1_000_000, Thinking: 400_000}, 20.0, false, false},
		{"unknown version uses closest known id", "claude-fable-5-2", parse.Usage{Input: 1_000_000}, 10.0, true, false},
		{"unknown version uses family default", "claude-opus-6", parse.Usage{Input: 1_000_000}, 4.0, true, false},
		{"fast mode uses base price", "claude-opus-5-5", parse.Usage{Input: 1_000_000, Speed: "fast"}, 4.0, true, false},
		{"no family match", "gpt-x", parse.Usage{Input: 1}, 0, false, true},
		{"empty model", "", parse.Usage{Input: 1}, 0, false, true},
	}
	for _, c := range cases {
		c.u.Model = c.model
		got, est, unk := Cost(c.u)
		if est != c.estimated || unk != c.unknown || math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: got %v,%v,%v want %v,%v,%v", c.name, got, est, unk, c.want, c.estimated, c.unknown)
		}
	}
}

func TestNormalizeModel(t *testing.T) {
	for in, want := range map[string]string{"claude-opus-5-5": "claude-opus-5-5", "claude-opus-5-5[1m]": "claude-opus-5-5",
		"claude-haiku-4-5-20251001": "claude-haiku-4-5", "<synthetic>": "<synthetic>"} {
		if got := NormalizeModel(in); got != want {
			t.Errorf("%q -> %q want %q", in, got, want)
		}
	}
}
