// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package buildinfo holds the version stamped in at build time.
package buildinfo

import "runtime/debug"

// Set via -ldflags "-X github.com/usebraid/braid/internal/buildinfo.Version=...".
var (
	Version = "dev"
	Commit  = ""
)

// Info returns the version and commit; an empty Commit falls back to the VCS revision of the build.
func Info() (version, commit string) {
	commit = Commit
	if commit == "" {
		if bi, ok := debug.ReadBuildInfo(); ok {
			for _, s := range bi.Settings {
				if s.Key == "vcs.revision" {
					commit = s.Value
				}
			}
		}
	}
	return Version, commit
}
