// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package client_test

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/usebraid/braid/internal/daemon/client"
	"github.com/usebraid/braid/internal/protocol"
)

func TestIngestSendsGzipAndHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer braid_dev_x" || r.Header.Get(protocol.HeaderVersion) != "1" || r.Header.Get("Content-Encoding") != "gzip" {
			t.Errorf("headers %v", r.Header)
		}
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "braid/") {
			t.Errorf("UA %q", r.Header.Get("User-Agent"))
		}
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var req protocol.IngestRequest
		if err := json.NewDecoder(zr).Decode(&req); err != nil || len(req.Files) != 1 {
			t.Errorf("%v %+v", err, req)
		}
		_ = json.NewEncoder(w).Encode(protocol.IngestResponse{Files: []protocol.FileResult{{Path: "p/a.jsonl", Status: protocol.StatusOK, EndOffset: 3}}})
	}))
	defer srv.Close()
	c := client.New(srv.URL, "braid_dev_x", "0.1.0", srv.Client())
	resp, err := c.Ingest(context.Background(), protocol.IngestRequest{Files: []protocol.FileBatch{{Kind: protocol.KindAppend, Path: "p/a.jsonl"}}})
	if err != nil || resp.Files[0].EndOffset != 3 {
		t.Fatalf("%+v %v", resp, err)
	}
}

func TestErrorClassification(t *testing.T) {
	cases := []struct {
		status int
		body   string
		check  func(error) bool
	}{
		{401, `{"error":"unauthorized"}`, func(e error) bool { return errors.Is(e, client.ErrUnauthorized) }},
		{426, `{"error":"protocol_mismatch","server":2,"min":2}`, func(e error) bool { var p *client.ProtocolMismatchError; return errors.As(e, &p) && p.Server == 2 }},
		{429, `{}`, func(e error) bool {
			var r *client.RetryableError
			return errors.As(e, &r) && r.RetryAfter == 7*time.Second
		}},
		{503, ``, func(e error) bool { var r *client.RetryableError; return errors.As(e, &r) }},
		{400, `{"error":"bad_request"}`, func(e error) bool { var p *client.PermanentError; return errors.As(e, &p) && p.Status == 400 }},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if c.status == 429 {
				w.Header().Set("Retry-After", "7")
			}
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		}))
		_, err := client.New(srv.URL, "t", "0", srv.Client()).Files(context.Background())
		if !c.check(err) {
			t.Errorf("status %d: %v", c.status, err)
		}
		srv.Close()
	}
	_, err := client.New("http://127.0.0.1:1", "t", "0", nil).Files(context.Background())
	var r *client.RetryableError
	if !errors.As(err, &r) {
		t.Errorf("network error not retryable: %v", err)
	}
}

func TestBackoff(t *testing.T) {
	b := client.Backoff{Min: time.Second, Max: 5 * time.Minute, Rand: func() float64 { return 0.999999 }}
	if d := b.Next(0); d < 990*time.Millisecond || d > time.Second {
		t.Errorf("0: %v", d)
	}
	if d := b.Next(3); d < 7*time.Second || d > 8*time.Second {
		t.Errorf("3: %v", d)
	}
	if d := b.Next(30); d > 5*time.Minute {
		t.Errorf("30: %v", d)
	}
	b.Rand = func() float64 { return 0 }
	if d := b.Next(3); d != 4*time.Second {
		t.Errorf("jitter floor: %v", d)
	}
}
