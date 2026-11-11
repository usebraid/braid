// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package ingest_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/usebraid/braid/internal/protocol"
	"github.com/usebraid/braid/internal/server/db"
	"github.com/usebraid/braid/internal/server/httpx"
	"github.com/usebraid/braid/internal/server/ingest"
	"github.com/usebraid/braid/internal/testdb"
)

// reply is a fully read HTTP response.
type reply struct {
	StatusCode int
	body       []byte
}

type env struct {
	owner, app               string
	srv                      *httptest.Server
	tokA, tokB, tokStatsOnly string
}

func newEnv(t *testing.T) *env { return newEnvWithLimit(t, 600) }

func newEnvWithLimit(t *testing.T, perMinute int) *env {
	t.Helper()
	owner, app := testdb.New(t)
	d, err := db.Open(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	e := &env{owner: owner, app: app, tokA: "braid_dev_tokA", tokB: "braid_dev_tokB", tokStatsOnly: "braid_dev_tokStats"}
	for i, u := range []struct {
		tok    string
		scopes string
		user   string
		dev    string
	}{
		{e.tokA, `{ingest:write,stats:read:self}`, "a", "a"},
		{e.tokStatsOnly, `{stats:read:self}`, "a", "a"},
		{e.tokB, `{ingest:write}`, "b", "b"},
	} {
		_ = i
		testdb.Exec(t, owner, `INSERT INTO users(oidc_issuer, oidc_sub) VALUES ('i', $1) ON CONFLICT DO NOTHING`, u.user)
		testdb.Exec(t, owner, `INSERT INTO devices(user_id, name, id)
			SELECT id, 'dev', md5($1)::uuid FROM users WHERE oidc_sub = $1 ON CONFLICT DO NOTHING`, u.dev)
		h := sha256.Sum256([]byte(u.tok))
		testdb.Exec(t, owner, `INSERT INTO device_tokens(device_id, user_id, hash, scopes)
			SELECT d.id, d.user_id, $2, $3::text[] FROM devices d WHERE d.id = md5($1)::uuid`, u.dev, h[:], u.scopes)
	}
	mux := http.NewServeMux()
	ingest.New(d).Register(mux, httpx.NewLimiter(perMinute, time.Now))
	e.srv = httptest.NewServer(mux)
	t.Cleanup(e.srv.Close)
	return e
}

func (e *env) do(t *testing.T, method, path, token string, body []byte, mutate func(*http.Request)) reply {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, e.srv.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set(protocol.HeaderVersion, "1")
	if method == http.MethodPost {
		req.Header.Set("Content-Encoding", "gzip")
	}
	if mutate != nil {
		mutate(req)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return reply{resp.StatusCode, b}
}

func (e *env) post(t *testing.T, token string, req protocol.IngestRequest, mutate func(*http.Request)) (reply, protocol.IngestResponse) {
	t.Helper()
	j, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(j)
	_ = zw.Close()
	resp := e.do(t, http.MethodPost, "/v1/ingest", token, buf.Bytes(), mutate)
	var out protocol.IngestResponse
	_ = json.Unmarshal(resp.body, &out)
	return resp, out
}

func (e *env) files(t *testing.T, token string) []protocol.RemoteFile {
	t.Helper()
	resp := e.do(t, http.MethodGet, "/v1/files", token, nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("files status %d", resp.StatusCode)
	}
	var out []protocol.RemoteFile
	if err := json.Unmarshal(resp.body, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func (e *env) ownerInt(t *testing.T, q string) int {
	t.Helper()
	c, err := pgx.Connect(context.Background(), e.owner)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	var n int
	if err := c.QueryRow(context.Background(), q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *env) countLines(t *testing.T) int { return e.ownerInt(t, `SELECT count(*) FROM raw_lines`) }
func (e *env) countSnapshots(t *testing.T) int {
	return e.ownerInt(t, `SELECT count(*) FROM raw_snapshots`)
}

func (e *env) firstLineBytes(t *testing.T, path string) []byte {
	t.Helper()
	c, err := pgx.Connect(context.Background(), e.owner)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	var b []byte
	err = c.QueryRow(context.Background(), `SELECT l.line FROM raw_lines l JOIN source_files f ON f.id = l.source_file_id
		WHERE f.rel_path = $1 ORDER BY l."offset" LIMIT 1`, path).Scan(&b)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (e *env) revokeTokenA(t *testing.T) {
	h := sha256.Sum256([]byte(e.tokA))
	testdb.Exec(t, e.owner, `UPDATE device_tokens SET revoked_at = now() WHERE hash = $1`, h[:])
}

func line(s string) protocol.Line { return protocol.EncodeLine([]byte(s)) }

var head = protocol.HashHex([]byte(`{"a":0}`))

func appendReq(from int64, lines ...string) protocol.IngestRequest {
	ls := make([]protocol.Line, len(lines))
	for i, l := range lines {
		ls[i] = line(l)
	}
	return protocol.IngestRequest{Files: []protocol.FileBatch{{Kind: protocol.KindAppend, Path: "p/s.jsonl", HeadHash: head, FromOffset: from, Lines: ls}}}
}

func TestIngestIdempotent(t *testing.T) {
	e := newEnv(t)
	_, r1 := e.post(t, e.tokA, appendReq(0, `{"a":0}`, `{"a":1}`), nil)
	if r1.Files[0].Status != protocol.StatusOK || r1.Files[0].EndOffset != 16 {
		t.Fatalf("%+v", r1)
	}
	_, r2 := e.post(t, e.tokA, appendReq(0, `{"a":0}`, `{"a":1}`), nil)
	if r2.Files[0].Status != protocol.StatusOK {
		t.Fatalf("%+v", r2)
	}
	if n := e.countLines(t); n != 2 {
		t.Fatalf("lines stored: %d", n)
	}
}

func TestIngestGap(t *testing.T) {
	e := newEnv(t)
	e.post(t, e.tokA, appendReq(0, `{"a":0}`), nil)
	_, r := e.post(t, e.tokA, appendReq(100, `{"a":9}`), nil)
	if r.Files[0].Status != protocol.StatusGap || r.Files[0].EndOffset != 8 {
		t.Fatalf("%+v", r)
	}
	if n := e.countLines(t); n != 1 {
		t.Fatalf("gap entry stored lines: %d", n)
	}
}

func TestIngestChangedLineAtSameOffsetKept(t *testing.T) {
	e := newEnv(t)
	e.post(t, e.tokA, appendReq(0, `{"a":0}`, `{"a":1}`), nil)
	e.post(t, e.tokA, appendReq(0, `{"a":0}`, `{"a":2}`), nil)
	if n := e.countLines(t); n != 3 {
		t.Fatalf("lines: %d, want 3 (changed line kept as extra row)", n)
	}
}

func TestIngestByteExact(t *testing.T) {
	e := newEnv(t)
	raw := []byte{'{', 0xff, 0x00, '}'}
	req := protocol.IngestRequest{Files: []protocol.FileBatch{{Kind: protocol.KindAppend, Path: "p/b.jsonl", HeadHash: protocol.HashHex(raw), Lines: []protocol.Line{protocol.EncodeLine(raw)}}}}
	e.post(t, e.tokA, req, nil)
	if got := e.firstLineBytes(t, "p/b.jsonl"); !bytes.Equal(got, raw) {
		t.Fatalf("stored %x", got)
	}
}

func TestSnapshotVersions(t *testing.T) {
	e := newEnv(t)
	snap := func(s string) protocol.IngestRequest {
		c := line(s)
		return protocol.IngestRequest{Files: []protocol.FileBatch{{Kind: protocol.KindSnapshot, Path: "p/s/subagents/agent-a.meta.json", Content: &c}}}
	}
	e.post(t, e.tokA, snap(`{"v":1}`), nil)
	e.post(t, e.tokA, snap(`{"v":1}`), nil)
	e.post(t, e.tokA, snap(`{"v":2}`), nil)
	if n := e.countSnapshots(t); n != 2 {
		t.Fatalf("snapshots: %d", n)
	}
	files := e.files(t, e.tokA)
	if len(files) != 1 || files[0].ContentHash != protocol.HashHex([]byte(`{"v":2}`)) {
		t.Fatalf("%+v", files)
	}
}

func TestMultiFileBatchAtomic(t *testing.T) {
	e := newEnv(t)
	req := appendReq(0, `{"a":0}`)
	req.Files = append(req.Files, protocol.FileBatch{Kind: protocol.KindSnapshot, Path: "p/s.jsonl", Content: &protocol.Line{}}) // invalid: kind/ext mismatch + bad content
	resp, _ := e.post(t, e.tokA, req, nil)
	if resp.StatusCode != 400 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if n := e.countLines(t); n != 0 {
		t.Fatalf("partial batch stored %d lines", n)
	}
}

func TestKindMismatchRollsBack(t *testing.T) {
	e := newEnv(t)
	e.post(t, e.tokA, appendReq(0, `{"a":0}`), nil)
	c := line("x")
	// same (path, head_hash) but a different kind can only come from a stale row; force one via owner SQL.
	testdb.Exec(t, e.owner, `UPDATE source_files SET kind = 'snapshot'`)
	req := appendReq(8, `{"a":1}`)
	req.Files = append(req.Files, protocol.FileBatch{Kind: protocol.KindSnapshot, Path: "p/o.json", Content: &c})
	resp, _ := e.post(t, e.tokA, req, nil)
	if resp.StatusCode != 400 || e.countSnapshots(t) != 0 || e.countLines(t) != 1 {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestHostilePathsRejected(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"../../etc/x.jsonl", "/abs.jsonl", `a\b.jsonl`, "a/\x00.jsonl"} {
		req := appendReq(0, `{"a":0}`)
		req.Files[0].Path = p
		resp, _ := e.post(t, e.tokA, req, nil)
		if resp.StatusCode != 400 {
			t.Errorf("%q: %d", p, resp.StatusCode)
		}
	}
	if n := e.countLines(t); n != 0 {
		t.Fatalf("stored %d lines", n)
	}
}

func TestAuthAndProtocolErrors(t *testing.T) {
	e := newEnv(t)
	if resp, _ := e.post(t, "", appendReq(0, `{}`), nil); resp.StatusCode != 401 {
		t.Errorf("no token: %d", resp.StatusCode)
	}
	if resp, _ := e.post(t, "braid_dev_wrong", appendReq(0, `{}`), nil); resp.StatusCode != 401 {
		t.Errorf("bad token: %d", resp.StatusCode)
	}
	if resp, _ := e.post(t, e.tokStatsOnly, appendReq(0, `{}`), nil); resp.StatusCode != 403 {
		t.Errorf("wrong scope: %d", resp.StatusCode)
	}
	resp, _ := e.post(t, e.tokA, appendReq(0, `{}`), func(r *http.Request) { r.Header.Set(protocol.HeaderVersion, "2") })
	if resp.StatusCode != 426 {
		t.Errorf("protocol: %d", resp.StatusCode)
	}
	resp, _ = e.post(t, e.tokA, appendReq(0, `{}`), func(r *http.Request) { r.Header.Del("Content-Encoding") })
	if resp.StatusCode != 415 {
		t.Errorf("no gzip: %d", resp.StatusCode)
	}
	e.revokeTokenA(t)
	if resp, _ := e.post(t, e.tokA, appendReq(0, `{}`), nil); resp.StatusCode != 401 {
		t.Errorf("revoked: %d", resp.StatusCode)
	}
}

func TestBodyLimits(t *testing.T) {
	e := newEnv(t)
	big := strings.Repeat("a", protocol.MaxDecompressedBody) // compresses to < 8 MiB, decompresses beyond limit
	resp, _ := e.post(t, e.tokA, appendReq(0, big), nil)
	if resp.StatusCode != 413 {
		t.Fatalf("zip bomb: %d", resp.StatusCode)
	}
}

func TestDevicesIsolated(t *testing.T) {
	e := newEnv(t)
	e.post(t, e.tokA, appendReq(0, `{"a":0}`), nil)
	if files := e.files(t, e.tokB); len(files) != 0 {
		t.Fatalf("B sees A's files: %+v", files)
	}
}

func TestRateLimit(t *testing.T) {
	e := newEnvWithLimit(t, 3)
	codes := []int{}
	for i := 0; i < 4; i++ {
		resp, _ := e.post(t, e.tokA, appendReq(0, `{"a":0}`), nil)
		codes = append(codes, resp.StatusCode)
	}
	if codes[3] != 429 {
		t.Fatalf("codes %v", codes)
	}
}
