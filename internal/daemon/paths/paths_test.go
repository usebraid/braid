// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package paths_test

import (
	"testing"

	"github.com/usebraid/braid/internal/daemon/paths"
)

func TestResolve(t *testing.T) {
	env := map[string]string{}
	get := func(k string) string { return env[k] }
	d := paths.Resolve("darwin", "/Users/u", get)
	if d.ClaudeProjects != "/Users/u/.claude/projects" || d.Data != "/Users/u/Library/Application Support/braid" {
		t.Fatalf("%+v", d)
	}
	env["CLAUDE_CONFIG_DIR"] = "/opt/claude"
	env["XDG_STATE_HOME"] = "/s"
	d = paths.Resolve("linux", "/home/u", get)
	if d.ClaudeProjects != "/opt/claude/projects" || d.Data != "/s/braid" || d.Config != "/home/u/.config/braid" {
		t.Fatalf("%+v", d)
	}
	env = map[string]string{"LOCALAPPDATA": `C:\Users\u\AppData\Local`, "APPDATA": `C:\Users\u\AppData\Roaming`}
	d = paths.Resolve("windows", `C:\Users\u`, get)
	if d.Data != `C:\Users\u\AppData\Local\braid` || d.Config != `C:\Users\u\AppData\Roaming\braid` {
		t.Fatalf("%+v", d)
	}
	d = paths.Resolve("windows", `C:\Users\u`, func(string) string { return "" })
	if d.Data != `C:\Users\u\AppData\Local\braid` || d.ClaudeProjects != `C:\Users\u\.claude\projects` {
		t.Fatalf("%+v", d)
	}
	env = map[string]string{"BRAID_CONFIG_DIR": "/c", "BRAID_DATA_DIR": "/d"}
	d = paths.Resolve("linux", "/home/u", get)
	if d.Config != "/c" || d.Data != "/d" || d.ClaudeProjects != "/home/u/.claude/projects" {
		t.Fatalf("%+v", d)
	}
}
