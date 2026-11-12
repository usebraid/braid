// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package derive

import "testing"

func TestProjectName(t *testing.T) {
	for in, want := range map[string]string{"/work/alpha": "alpha", "/work/alpha/": "alpha", `C:\work\beta`: "beta", `C:\work\beta\`: "beta", "gamma": "gamma", "": "", "/": ""} {
		if got := projectName(in); got != want {
			t.Errorf("%q -> %q want %q", in, got, want)
		}
	}
}
