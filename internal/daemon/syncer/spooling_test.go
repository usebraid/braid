// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package syncer_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usebraid/braid/internal/daemon/client"
	"github.com/usebraid/braid/internal/protocol"
)

type downUp struct{ err error }

func (d downUp) Ingest(context.Context, protocol.IngestRequest) (protocol.IngestResponse, error) {
	return protocol.IngestResponse{}, d.err
}
func (d downUp) Files(context.Context) ([]protocol.RemoteFile, error) { return nil, d.err }

// Offline, every unacked file and snapshot is spooled, not only the first batch.
func TestOfflineSpoolsEverythingAndSurvivesCleanup(t *testing.T) {
	fs := newFakeServer()
	e, root := newEngine(t, fs)
	for i := 0; i < 6; i++ {
		appendTo(t, root, fmt.Sprintf("p/s%d/agent.meta.json", i), fmt.Sprintf(`{"agentType":"x","n":%d,"pad":"%s"}`, i, strings.Repeat("x", 40)))
		appendTo(t, root, fmt.Sprintf("p/t%d.jsonl", i), fmt.Sprintf("head%d\nline\n", i))
	}
	e.Up = downUp{&client.RetryableError{Status: 503}}
	for i := 0; i < 3; i++ {
		_, _ = e.Pass(context.Background())
	}
	_ = os.RemoveAll(filepath.Join(root, "p")) // Claude Code cleanup
	e.Up = fs
	for i := 0; i < 3; i++ {
		_, _ = e.Pass(context.Background())
	}
	for i := 0; i < 6; i++ {
		if fs.snapshots(fmt.Sprintf("p/s%d/agent.meta.json", i)) != 1 || len(fs.lines(fmt.Sprintf("p/t%d.jsonl", i))) != 2 {
			t.Fatalf("file %d lost", i)
		}
	}
}

// A rejected token on /v1/files (first pass) must not stop spooling.
func TestSpoolsBeforeFirstSuccessfulResume(t *testing.T) {
	e, root := newEngine(t, newFakeServer())
	appendTo(t, root, "p/a.jsonl", "h\n1\n")
	appendTo(t, root, "p/m.json", `{"a":1}`)
	e.Up = downUp{client.ErrUnauthorized}
	if _, err := e.Pass(context.Background()); err == nil {
		t.Fatal("want the resume error")
	}
	_ = os.RemoveAll(filepath.Join(root, "p"))
	fs := newFakeServer()
	e.Up = fs
	for i := 0; i < 3; i++ {
		_, _ = e.Pass(context.Background())
	}
	if len(fs.lines("p/a.jsonl")) != 2 || fs.snapshots("p/m.json") != 1 {
		t.Fatal("data not spooled before the first resume")
	}
}
