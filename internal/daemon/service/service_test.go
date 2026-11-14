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
	got, err := service.Environ([]string{"PATH=/bin", "BRAID_TOKEN=s", "BRAID_API_TOKEN=s", "BRAID_OIDC_CLIENT_SECRET=s3cr3t", "BRAID_DATABASE_URL=postgres://u:pw@h/db",
		"BRAID_DATA_DIR=/d", "BRAID_CONFIG_DIR=/cfg", "CLAUDE_CONFIG_DIR=/c", "XDG_DATA_HOME=", "XDG_CACHE_HOME=/x", "HOME=/h"})
	if err != nil || strings.Join(got, ",") != "BRAID_CONFIG_DIR=/cfg,BRAID_DATA_DIR=/d,CLAUDE_CONFIG_DIR=/c,XDG_CACHE_HOME=/x" {
		t.Fatalf("%v %v", got, err)
	}
}

func TestEnvironRejectsBadValuesAndKeys(t *testing.T) {
	for _, kv := range []string{"CLAUDE_CONFIG_DIR=/a\nExecStartPre=/tmp/evil", "BRAID_DATA_DIR=/a\x00b", "XDG_DATA_HOME=/a\rb"} {
		if _, err := service.Environ([]string{kv}); err == nil {
			t.Errorf("%q accepted", kv)
		}
	}
	for _, key := range []string{"BRAID_A\nExecStartPre=/tmp/evil", "lower", "A-B", "1A", ""} {
		for _, goos := range []string{"linux", "darwin", "windows"} {
			if _, _, err := service.Render(service.Spec{GOOS: goos, Exe: "/b", User: "u", Env: []string{key + "=1"}}); err == nil {
				t.Errorf("%s: key %q accepted", goos, key)
			}
		}
	}
}

func TestWindowsRefusesPercentInExePathWithEnv(t *testing.T) {
	if _, _, err := service.Render(service.Spec{GOOS: "windows", Exe: `C:\%PATH%\braid.exe`, User: "u", Env: []string{"BRAID_DATA_DIR=d"}}); err == nil {
		t.Error("% in the exe path accepted")
	}
	if _, _, err := service.Render(service.Spec{GOOS: "windows", Exe: `C:\braid.exe`, DataDir: `C:\%TEMP%\braid`, User: "u"}); err == nil {
		t.Error("% in the data dir accepted")
	}
}

func TestServiceFilesAreOwnerOnlyAndExistingOnesAreFixed(t *testing.T) {
	run := func(string, ...string) error { return nil }
	home := t.TempDir()
	path := home + "/.config/systemd/user/braid.service"
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil { //nolint:gosec // simulates a file from an older version
		t.Fatal(err)
	}
	if err := service.Install(service.Spec{GOOS: "linux", Exe: "/b", Home: home, DataDir: home}, run); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", fi, err)
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
