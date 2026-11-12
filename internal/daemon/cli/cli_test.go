// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package cli_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/usebraid/braid/internal/daemon/cli"
)

func TestVersionAndUnknown(t *testing.T) {
	var out, errb bytes.Buffer
	if code := cli.Run(context.Background(), []string{"version"}, nil, &out, &errb); code != 0 || !strings.Contains(out.String(), "protocol 1") {
		t.Fatalf("%d %s", code, out.String())
	}
	if code := cli.Run(context.Background(), []string{"nope"}, nil, &out, &errb); code != 2 {
		t.Fatalf("unknown: %d", code)
	}
}

func TestLoginRefusesFileStoreWithoutConsent(t *testing.T) {
	var out, errb bytes.Buffer
	code := cli.RunWithHooks(context.Background(), []string{"login", "https://braid.example"}, strings.NewReader("n\n"), &out, &errb, cli.Hooks{NoKeychain: true, FakeToken: "braid_dev_a", ConfigDir: t.TempDir(), DataDir: t.TempDir()}) //nolint:gosec // G101: test fixture
	if code != 1 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	code = cli.RunWithHooks(context.Background(), []string{"login", "https://braid.example"}, strings.NewReader("y\n"), &out, &errb, cli.Hooks{NoKeychain: true, FakeToken: "braid_dev_a", ConfigDir: t.TempDir(), DataDir: t.TempDir()}) //nolint:gosec // G101: test fixture
	if code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
}

func TestLoginRejectsPlainHTTP(t *testing.T) {
	var out, errb bytes.Buffer
	if code := cli.Run(context.Background(), []string{"login", "http://braid.example"}, nil, &out, &errb); code == 0 {
		t.Fatal("plain http accepted")
	}
}

func TestStatusNotLoggedIn(t *testing.T) {
	var out, errb bytes.Buffer
	code := cli.RunWithHooks(context.Background(), []string{"status"}, nil, &out, &errb, cli.Hooks{ConfigDir: t.TempDir(), DataDir: t.TempDir()})
	if code != 2 || !strings.Contains(out.String(), "not logged in") {
		t.Fatalf("%d %s", code, out.String())
	}
}
