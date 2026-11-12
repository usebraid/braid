// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package paths resolves the OS-specific directories used by the daemon.
package paths

// Dirs holds the directories the daemon reads and writes.
type Dirs struct{ ClaudeProjects, Data, Config string }

// Resolve builds paths with the separator of the target goos, not the host.
// BRAID_CONFIG_DIR and BRAID_DATA_DIR override Config and Data for tests and unusual setups.
func Resolve(goos, home string, getenv func(string) string) Dirs {
	sep := "/"
	if goos == "windows" {
		sep = `\`
	}
	join := func(parts ...string) string {
		s := parts[0]
		for _, p := range parts[1:] {
			s += sep + p
		}
		return s
	}
	env := func(k, def string) string {
		if v := getenv(k); v != "" {
			return v
		}
		return def
	}
	var d Dirs
	d.ClaudeProjects = join(env("CLAUDE_CONFIG_DIR", join(home, ".claude")), "projects")
	switch goos {
	case "darwin":
		d.Data = join(home, "Library", "Application Support", "braid")
		d.Config = d.Data
	case "windows":
		d.Data = join(env("LOCALAPPDATA", join(home, "AppData", "Local")), "braid")
		d.Config = join(env("APPDATA", join(home, "AppData", "Roaming")), "braid")
	default:
		d.Data = join(env("XDG_STATE_HOME", join(home, ".local", "state")), "braid")
		d.Config = join(env("XDG_CONFIG_HOME", join(home, ".config")), "braid")
	}
	d.Data, d.Config = env("BRAID_DATA_DIR", d.Data), env("BRAID_CONFIG_DIR", d.Config)
	return d
}
