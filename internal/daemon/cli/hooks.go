// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package cli

import (
	"context"
	"io"
)

// Hooks override environment lookups. Only meant for tests; Run uses the zero value.
type Hooks struct {
	NoKeychain bool   // keychain Set fails with auth.ErrNoKeychain
	FakeToken  string // skip the device flow and use this token
	ConfigDir  string // overrides the config dir
	DataDir    string // overrides the data dir
}

// Run dispatches subcommands; returns the process exit code.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return RunWithHooks(ctx, args, stdin, stdout, stderr, Hooks{})
}
