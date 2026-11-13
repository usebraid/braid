// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// The price table and cost formula are ported from ponytail
// (https://github.com/DietrichGebert/ponytail, hooks/ponytail-band-import.js),
// Copyright (c) 2026 DietrichGebert, MIT License; the license text is in
// THIRD_PARTY_NOTICES.md. Prices are USD per million tokens from the Anthropic
// pricing page as checked by ponytail on 2026-10-17; updates are code changes.

// Package derive turns parsed transcript events into derived facts such as cost.
package derive

import (
	"regexp"
	"strings"

	"github.com/usebraid/braid/internal/server/derive/parse"
)

// Price is USD per million tokens.
type Price struct{ Input, CacheWrite5m, CacheWrite1h, CacheRead, Output float64 }

var (
	opusOld = Price{5, 6.25, 10, 0.5, 25}
	prices  = map[string]Price{
		"claude-fable-5-1":  {10, 12.5, 20, 0.25, 50},
		"claude-fable-5":    {10, 12.5, 20, 1, 50},
		"claude-opus-5-5":   {4, 5, 8, 0.2, 20},
		"claude-opus-5":     opusOld,
		"claude-opus-4-8":   opusOld,
		"claude-opus-4-7":   opusOld,
		"claude-opus-4-6":   opusOld,
		"claude-opus-4-5":   opusOld,
		"claude-sonnet-5-5": {2, 2.5, 4, 0.1, 10},
		"claude-sonnet-5":   {2, 2.5, 4, 0.2, 10},
		"claude-sonnet-4-6": {3, 3.75, 6, 0.3, 15},
		"claude-sonnet-4-5": {3, 3.75, 6, 0.3, 15},
		"claude-haiku-5-5":  {0.1, 0.125, 0.2, 0.01, 0.5},
		"claude-haiku-4-5":  {1, 1.25, 2, 0.1, 5},
	}
	// familyDefault is the newest known model of a family, the fallback for unknown versions.
	familyDefault = map[string]string{
		"claude-fable-":  "claude-fable-5-1",
		"claude-opus-":   "claude-opus-5-5",
		"claude-sonnet-": "claude-sonnet-5-5",
		"claude-haiku-":  "claude-haiku-5-5",
	}
	haikuLong  = Price{0.5, 0.625, 1, 0.05, 2.5}
	dateSuffix = regexp.MustCompile(`-\d{8}$`)
)

// NormalizeModel strips a trailing "[...]" and a trailing "-YYYYMMDD".
func NormalizeModel(m string) string {
	if strings.HasSuffix(m, "]") {
		if i := strings.LastIndex(m, "["); i >= 0 {
			m = m[:i]
		}
	}
	return dateSuffix.ReplaceAllString(m, "")
}

// lookup finds the price of m: exact, else the longest known id prefix at a "-" boundary
// (claude-fable-5-2 -> claude-fable-5), else the family default. ok is false without a family match;
// exact reports whether the model itself is in the table.
func lookup(m string) (id string, exact, ok bool) {
	if _, ok := prices[m]; ok {
		return m, true, true
	}
	for s := m; strings.Contains(s, "-"); {
		s = s[:strings.LastIndex(s, "-")]
		if _, ok := prices[s]; ok {
			return s, false, true
		}
	}
	for prefix, def := range familyDefault {
		if strings.HasPrefix(m, prefix) {
			return def, false, true
		}
	}
	if strings.HasPrefix(m, "claude-3") { // legacy scheme claude-3-5-sonnet: family comes last
		for _, f := range []string{"fable", "opus", "sonnet", "haiku"} {
			if strings.Contains(m, "-"+f) {
				return familyDefault["claude-"+f+"-"], false, true
			}
		}
	}
	return "", false, false
}

// Cost prices one message in USD. estimated is true when the base price of the model, or of its closest
// known family member, was used for an unknown model or for Speed == "fast". unknown is true (and usd 0)
// only when no family matches at all.
func Cost(u parse.Usage) (usd float64, estimated, unknown bool) {
	id, exact, ok := lookup(NormalizeModel(u.Model))
	if !ok {
		return 0, false, true
	}
	p := prices[id]
	if id == "claude-haiku-5-5" && u.Input+u.CacheRead+u.CacheWrite5m+u.CacheWrite1h > 100000 {
		p = haikuLong
	}
	usd = (float64(u.Input)*p.Input+float64(u.CacheWrite5m)*p.CacheWrite5m+float64(u.CacheWrite1h)*p.CacheWrite1h+
		float64(u.CacheRead)*p.CacheRead+float64(u.Output)*p.Output)/1e6 + float64(u.WebSearch)*0.01 // Thinking is part of Output
	return usd, !exact || u.Speed == "fast", false
}
