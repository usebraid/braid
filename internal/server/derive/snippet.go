// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package derive

import (
	"slices"
	"strings"
	"unicode"

	"github.com/usebraid/braid/internal/server/derive/parse"
)

const snippetRunes = 160

// Snippet cuts a short plain-text excerpt for query q out of the raw line of a search hit (role "user" or "assistant").
// Candidate texts: user = the prompt; assistant = each text block, then each tool call as "<tool> <summary>".
// The first candidate containing a query word (case-insensitive substring of any word of q) wins; the excerpt is a window of
// at most 160 runes around the match with "…" at cut ends and whitespace collapsed. No match: the start of the first candidate.
// Unparsable or empty input returns "".
func Snippet(raw []byte, role, q string) string {
	evs, err := parse.ParseLine(raw)
	if err != nil {
		return ""
	}
	var cands [][]rune
	for _, e := range evs {
		var s string
		switch {
		case role == "user" && e.Kind == parse.KindPrompt, role != "user" && e.Kind == parse.KindAssistant:
			s = e.Text
		case role != "user" && e.Kind == parse.KindToolUse:
			s = e.Tool + " " + e.Summary
		}
		if s = strings.Join(strings.Fields(s), " "); s != "" {
			cands = append(cands, []rune(s))
		}
	}
	var words [][]rune
	for _, w := range strings.Fields(q) {
		words = append(words, lowerRunes([]rune(w)))
	}
	for _, c := range cands {
		low := lowerRunes(c)
		for _, w := range words {
			if i := indexRunes(low, w); i >= 0 {
				return window(c, max(0, i-(snippetRunes-len(w))/2))
			}
		}
	}
	if len(cands) > 0 {
		return window(cands[0], 0)
	}
	return ""
}

// lowerRunes keeps the rune count, so indexes into the lowered copy are valid for the original.
func lowerRunes(r []rune) []rune {
	out := make([]rune, len(r))
	for i, c := range r {
		out[i] = unicode.ToLower(c)
	}
	return out
}

func indexRunes(s, sub []rune) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if slices.Equal(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

func window(r []rune, start int) string {
	end := min(len(r), start+snippetRunes)
	start = max(0, end-snippetRunes)
	s := string(r[start:end])
	if start > 0 {
		s = "…" + s
	}
	if end < len(r) {
		s += "…"
	}
	return s
}
