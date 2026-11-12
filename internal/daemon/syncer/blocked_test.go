// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package syncer_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usebraid/braid/internal/daemon/client"
	"github.com/usebraid/braid/internal/protocol"
)

// rejectUp answers 413 for any request that carries a line longer than max.
type rejectUp struct {
	*fakeServer
	max int
}

func (p rejectUp) Ingest(ctx context.Context, req protocol.IngestRequest) (protocol.IngestResponse, error) {
	for _, f := range req.Files {
		for _, l := range f.Lines {
			if b, _ := l.Bytes(); len(b) > p.max {
				return protocol.IngestResponse{}, &client.PermanentError{Status: 413, Body: "too_large"}
			}
		}
	}
	return p.fakeServer.Ingest(ctx, req)
}

func TestRejectedLineDoesNotBlockOtherFiles(t *testing.T) {
	fs := newFakeServer()
	e, root := newEngine(t, fs)
	appendTo(t, root, "a/1.jsonl", "h1\nx\n")
	appendTo(t, root, "b/poison.jsonl", "h2\n"+strings.Repeat("P", 500)+"\n")
	appendTo(t, root, "c/2.jsonl", "h3\ny\n")
	e.Up = rejectUp{fs, 400}
	_, err := e.Pass(context.Background())
	if err == nil {
		t.Fatal("pass must report the blocked file")
	}
	if len(fs.lines("a/1.jsonl")) == 0 || len(fs.lines("c/2.jsonl")) == 0 {
		t.Fatalf("healthy files stalled: a=%v c=%v", fs.lines("a/1.jsonl"), fs.lines("c/2.jsonl"))
	}
	if v, _, _ := e.State.Setting(context.Background(), "blocked"); v == "" {
		t.Fatal("blocked setting not set for braid status")
	}
}

func TestUnreadableFileDoesNotBlockOtherFiles(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read everything")
	}
	fs := newFakeServer()
	e, root := newEngine(t, fs)
	appendTo(t, root, "a/1.jsonl", "h1\nx\n")
	appendTo(t, root, "b/locked.jsonl", "h2\nz\n")
	appendTo(t, root, "c/2.jsonl", "h3\ny\n")
	if err := os.Chmod(filepath.Join(root, "b/locked.jsonl"), 0); err != nil {
		t.Fatal(err)
	}
	_, err := e.Pass(context.Background())
	if err == nil {
		t.Fatal("pass must report the unreadable file")
	}
	if len(fs.lines("a/1.jsonl")) == 0 || len(fs.lines("c/2.jsonl")) == 0 {
		t.Fatal("healthy files stalled")
	}
}
