// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package buildinfo_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usebraid/braid/internal/buildinfo"
)

const pkg = "github.com/usebraid/braid/internal/buildinfo"

// buildBin builds a main package with the test ldflags into a temp dir.
func buildBin(t *testing.T, name, main string) string {
	t.Helper()
	if testing.Short() {
		t.Skip("builds binaries")
	}
	bin := filepath.Join(t.TempDir(), name)
	build := exec.CommandContext(t.Context(), "go", "build", "-ldflags", //nolint:gosec // G204: fixed test args
		"-X "+pkg+".Version=9.9.9-test -X "+pkg+".Commit=abc123def", "-o", bin, main)
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

// Builds both binaries with ldflags into t.TempDir() and runs `version`.
func TestLdflagsStampBothBinaries(t *testing.T) {
	for _, c := range []struct{ name, main, want string }{
		{"braid", "../../cmd/braid", "braid 9.9.9-test (abc123def) protocol "},
		{"braid-server", "../../cmd/braid-server", "braid-server 9.9.9-test (abc123def)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			bin := buildBin(t, c.name, c.main)
			out, err := exec.CommandContext(t.Context(), bin, "version").Output() //nolint:gosec // G204: binary built by this test
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(out), c.want) {
				t.Fatalf("got %q, want prefix %q", out, c.want)
			}
		})
	}
}

func TestServerVersionIgnoresEnv(t *testing.T) {
	bin := buildBin(t, "braid-server", "../../cmd/braid-server")
	for _, flag := range []string{"--version", "-v"} {
		cmd := exec.CommandContext(t.Context(), bin, flag) //nolint:gosec // G204: binary built by this test
		cmd.Env = []string{}
		out, err := cmd.Output()
		if err != nil || len(out) == 0 {
			t.Fatalf("%s: %v %q", flag, err, out)
		}
	}
}

func TestDefaultsToDev(t *testing.T) {
	if buildinfo.Version != "dev" {
		t.Fatalf("Version = %q", buildinfo.Version)
	}
}
