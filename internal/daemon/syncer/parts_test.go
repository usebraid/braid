// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package syncer_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/usebraid/braid/internal/daemon/client"
	"github.com/usebraid/braid/internal/protocol"
)

func TestOversizedLineIsChunkedAndTheRestSyncs(t *testing.T) {
	fs := newFakeServer()
	e, root := newEngine(t, fs)
	big := strings.Repeat("é\"", 2<<20) // 8 MiB of text that escapes
	appendTo(t, root, "p/a.jsonl", "head\nbefore\n"+big+"\nafter\n")
	if _, err := e.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := fs.lines("p/a.jsonl")
	if len(got) != 4 || got[2] != big || got[3] != "after" {
		t.Fatalf("lines: %d", len(got))
	}
	if sk, _ := e.State.ListSkipped(context.Background()); len(sk) != 0 {
		t.Fatalf("skipped %v", sk)
	}
}

func TestChunkedLineResumesAfterARetryableError(t *testing.T) {
	fs := newFakeServer()
	fs.failOnPart = 2
	e, root := newEngine(t, fs)
	big := strings.Repeat("x", 4<<20)
	appendTo(t, root, "p/a.jsonl", "head\n"+big+"\n")
	var rerr *client.RetryableError
	if _, err := e.Pass(context.Background()); !errors.As(err, &rerr) {
		t.Fatalf("want retryable, got %v", err)
	}
	if _, err := e.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fs.lines("p/a.jsonl"); len(got) != 2 || got[1] != big {
		t.Fatal("line not stored")
	}
	if fs.partCalls[0] != 1 || fs.partCalls[1] != 1 {
		t.Fatalf("stored chunks were sent again: %v", fs.partCalls)
	}
}

func TestOversizedSnapshotIsChunked(t *testing.T) {
	fs := newFakeServer()
	e, root := newEngine(t, fs)
	content := `{"k":"` + strings.Repeat("v", 3<<20) + `"}`
	appendTo(t, root, "p/a/x.meta.json", content)
	if _, err := e.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := fs.snaps["p/a/x.meta.json"]; len(s) != 1 || s[0] != sum([]byte(content)) {
		t.Fatalf("snapshot %v", s)
	}
}

func TestLineAboveTheHardLimitIsSkippedAndKept(t *testing.T) {
	fs := newFakeServer()
	e, root := newEngine(t, fs)
	e.MaxLine = 2 << 20
	big := strings.Repeat("z", 3<<20)
	appendTo(t, root, "p/a.jsonl", "head\nbefore\n"+big+"\nafter\n")
	if _, err := e.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fs.lines("p/a.jsonl"); len(got) != 3 || got[1] != "before" || got[2] != "after" {
		t.Fatalf("lines %d", len(got))
	}
	sk, _ := e.State.ListSkipped(context.Background())
	if len(sk) != 1 || sk[0].RelPath != "p/a.jsonl" || sk[0].Offset != 12 || sk[0].Length != int64(len(big))+1 || sk[0].SHA256 != sum([]byte(big)) {
		t.Fatalf("skipped %+v", sk)
	}
	rows, _ := e.State.List(context.Background())
	if len(rows) != 1 || rows[0].SpoolPath == "" {
		t.Fatalf("row %+v", rows)
	}
	if _, err := os.Stat(rows[0].SpoolPath); err != nil {
		t.Fatalf("spool copy gone: %v", err)
	}
	_ = os.Remove(root + "/p/a.jsonl") // even with the original gone, the copy stays
	if _, err := e.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, _ = e.State.List(context.Background())
	if len(rows) != 1 {
		t.Fatalf("row dropped: %+v", rows)
	}
	if _, err := os.Stat(rows[0].SpoolPath); err != nil { // the entry may have been moved aside, never removed
		t.Fatalf("spool copy collected: %v", err)
	}
}

// quotaUp answers every part with the server's pending-parts quota error.
type quotaUp struct{ *fakeServer }

func (p quotaUp) Ingest(ctx context.Context, req protocol.IngestRequest) (protocol.IngestResponse, error) {
	for _, f := range req.Files {
		if (len(f.Lines) == 1 && f.Lines[0].Part != nil) || (f.Content != nil && f.Content.Part != nil) {
			return protocol.IngestResponse{}, &client.RetryableError{Status: 413, RetryAfter: time.Hour}
		}
	}
	return p.fakeServer.Ingest(ctx, req)
}

func TestPartsQuotaOnlyPausesTheChunkedUpload(t *testing.T) {
	fs := newFakeServer()
	e, root := newEngine(t, fs)
	appendTo(t, root, "a/big.jsonl", "h1\n"+strings.Repeat("x", 3<<20)+"\n")
	appendTo(t, root, "b/small.jsonl", "h2\nsmall\n")
	appendTo(t, root, "0/small.jsonl", "h3\nsmall\n")
	appendTo(t, root, "z/small.jsonl", "h4\nsmall\n")
	e.Up = quotaUp{fs}
	_, err := e.Pass(context.Background())
	if err == nil { // the paused upload is reported
		t.Fatal("pause not reported")
	}
	for _, f := range []string{"0/small.jsonl", "b/small.jsonl", "z/small.jsonl"} {
		if got := fs.lines(f); len(got) != 2 {
			t.Fatalf("%s stalled: %v", f, got)
		}
	}
}

// poisonUp rejects chunk 1 permanently n times, as a poisoned leftover on the server would.
type poisonUp struct {
	*fakeServer
	n int
}

func (p *poisonUp) Ingest(ctx context.Context, req protocol.IngestRequest) (protocol.IngestResponse, error) {
	for _, f := range req.Files {
		if len(f.Lines) == 1 && f.Lines[0].Part != nil && f.Lines[0].Part.Index == 1 && (p.n < 0 || p.n > 0) {
			if p.n > 0 {
				p.n--
			}
			return protocol.IngestResponse{}, &client.PermanentError{Status: 400, Body: "bad_request"}
		}
	}
	return p.fakeServer.Ingest(ctx, req)
}

func TestRejectedChunkRestartsTheSeriesOnce(t *testing.T) {
	fs := newFakeServer()
	e, root := newEngine(t, fs)
	big := strings.Repeat("x", 3<<20)
	appendTo(t, root, "a/big.jsonl", "h1\n"+big+"\n")
	e.Up = &poisonUp{fakeServer: fs, n: 2} // the request and its isolated resend fail, the restart works
	if _, err := e.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fs.lines("a/big.jsonl"); len(got) != 2 || got[1] != big {
		t.Fatal("line not stored after the restart")
	}
}

func TestChunkRejectedAgainAfterTheRestartBlocksTheFile(t *testing.T) {
	fs := newFakeServer()
	e, root := newEngine(t, fs)
	appendTo(t, root, "a/big.jsonl", "h1\n"+strings.Repeat("x", 3<<20)+"\n")
	appendTo(t, root, "b/small.jsonl", "h2\nsmall\n")
	e.Up = &poisonUp{fakeServer: fs, n: -1}
	if _, err := e.Pass(context.Background()); err == nil {
		t.Fatal("file must be reported as blocked")
	}
	if len(fs.lines("b/small.jsonl")) != 2 {
		t.Fatal("other file stalled")
	}
}
