// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package main

import (
	"errors"
	"testing"
)

func TestUnknownArgumentsAreUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"bogus"}, {"--help"}, {"version", "x"}, {"reindex", "--nope"}, {"reindex", "extra"}, {"braid-server", "reindex"}} {
		if err := run(args); !errors.Is(err, errUsage) {
			t.Errorf("%v: %v", args, err)
		}
	}
	if err := run([]string{"version"}); err != nil {
		t.Errorf("version: %v", err)
	}
}
