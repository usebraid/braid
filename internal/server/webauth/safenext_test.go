// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package webauth

import "testing"

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{
		"/x?a=1": "/x?a=1", "/%09/evil.example": "/%09/evil.example", "": "/",
		"/\t/evil.example": "/", "/\n/evil.example": "/", "/\\evil.example": "/", "\\\\evil.example": "/",
		"https://evil.example": "/", " /x": "/", "//evil.example": "/", "/\x7f/x": "/",
	} {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}
