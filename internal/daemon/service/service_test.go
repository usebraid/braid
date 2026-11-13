// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package service_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usebraid/braid/internal/daemon/service"
)

var update = flag.Bool("update", false, "update golden files")

func TestRender(t *testing.T) {
	for _, s := range []service.Spec{
		{GOOS: "darwin", Exe: "/opt/homebrew/bin/braid", Home: "/Users/u", DataDir: "/Users/u/Library/Application Support/braid"},
		{GOOS: "linux", Exe: "/usr/local/bin/braid", Home: "/home/u", DataDir: "/home/u/.local/state/braid"},
		{GOOS: "windows", Exe: `C:\Users\u\AppData\Local\braid\braid.exe`, Home: `C:\Users\u`, DataDir: `C:\Users\u\AppData\Local\braid`, User: `DOMAIN\jane`},
	} {
		path, content, err := service.Render(s)
		if err != nil {
			t.Fatal(err)
		}
		golden := filepath.Join("testdata", s.GOOS+".golden")
		if *update {
			_ = os.WriteFile(golden, append([]byte(path+"\n"), content...), 0o644) //nolint:gosec // G306: golden file
		}
		want, _ := os.ReadFile(golden) //nolint:gosec // G304: fixed testdata path
		if string(want) != path+"\n"+string(content) {
			t.Errorf("%s differs from golden", s.GOOS)
		}
		if !strings.Contains(string(content), s.Exe) || !strings.Contains(string(content), "run") {
			t.Errorf("%s: exe/run missing", s.GOOS)
		}
	}
}

var testEnv = []string{"CLAUDE_CONFIG_DIR=/w/my claude", "XDG_STATE_HOME=/s", "BRAID_URL=https://b.example/%x"}

func TestRenderEnv(t *testing.T) {
	for _, s := range []service.Spec{
		{GOOS: "darwin", Exe: "/opt/homebrew/bin/braid", Home: "/Users/u", DataDir: "/Users/u/d", Env: testEnv},
		{GOOS: "linux", Exe: "/usr/local/bin/braid", Home: "/home/u", DataDir: "/home/u/d", Env: testEnv},
		{GOOS: "windows", Exe: `C:\braid.exe`, Home: `C:\Users\u`, DataDir: `C:\d`, User: `DOMAIN\jane`, Env: testEnv[:2]},
	} {
		path, content, err := service.Render(s)
		if err != nil {
			t.Fatal(err)
		}
		golden := filepath.Join("testdata", s.GOOS+"-env.golden")
		if *update {
			_ = os.WriteFile(golden, append([]byte(path+"\n"), content...), 0o644) //nolint:gosec // G306: golden file
		}
		want, _ := os.ReadFile(golden) //nolint:gosec // G304: fixed testdata path
		if string(want) != path+"\n"+string(content) {
			t.Errorf("%s differs from golden", s.GOOS)
		}
	}
	if _, _, err := service.Render(service.Spec{GOOS: "windows", Exe: "x", User: "u", Env: []string{`BRAID_URL=a"b`}}); err == nil {
		t.Error("windows must refuse a quote in a value")
	}
	if _, _, err := service.Render(service.Spec{GOOS: "windows", Exe: "x"}); err == nil {
		t.Error("windows needs an account name")
	}
}

func TestEnviron(t *testing.T) {
	got := service.Environ([]string{"PATH=/bin", "BRAID_TOKEN=s", "BRAID_API_TOKEN=s", "BRAID_URL=u", "CLAUDE_CONFIG_DIR=/c", "XDG_DATA_HOME=", "XDG_CACHE_HOME=/x", "HOME=/h"})
	if strings.Join(got, ",") != "BRAID_URL=u,CLAUDE_CONFIG_DIR=/c,XDG_CACHE_HOME=/x" {
		t.Fatalf("%v", got)
	}
}

func TestInstallCommands(t *testing.T) {
	var cmds []string
	run := func(n string, a ...string) error { cmds = append(cmds, n+" "+strings.Join(a, " ")); return nil }
	home := t.TempDir()
	if err := service.Install(service.Spec{GOOS: "linux", Exe: "/b", Home: home, DataDir: home}, run); err != nil {
		t.Fatal(err)
	}
	if strings.Join(cmds, ";") != "systemctl --user daemon-reload;systemctl --user enable --now braid.service" {
		t.Fatalf("%v", cmds)
	}
}
