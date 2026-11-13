// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package ingest_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/usebraid/braid/internal/daemon/client"
	"github.com/usebraid/braid/internal/protocol"
	"github.com/usebraid/braid/internal/server/db"
	"github.com/usebraid/braid/internal/server/httpx"
	"github.com/usebraid/braid/internal/server/ingest"
	"github.com/usebraid/braid/internal/server/live"
	"github.com/usebraid/braid/internal/testdb"
)

// reply is a fully read HTTP response.
type reply struct {
	StatusCode int
	body       []byte
}

type env struct {
	owner, app                      string
	srv                             *httptest.Server
	h                               *ingest.Handler
	tokA, tokA2, tokB, tokStatsOnly string
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
	e := &env{owner: owner, app: app, tokA: "braid_dev_tokA", tokA2: "braid_dev_tokA2", tokB: "braid_dev_tokB", tokStatsOnly: "braid_dev_tokStats"}
	for i, u := range []struct {
		tok    string
		scopes string
		user   string
		dev    string
	}{
		{e.tokA, `{ingest:write,stats:read:self}`, "a", "a"},
		{e.tokA2, "{ingest:write}", "a", "a2"},
		{e.tokStatsOnly, `{stats:read:self}`, "a", "a"},
		{e.tokB, `{ingest:write}`, "b", "b"},
	} {
		_ = i
		testdb.Exec(t, owner, `INSERT INTO users(oidc_issuer, oidc_sub) VALUES ('i', $1) ON CONFLICT DO NOTHING`, u.user)
		testdb.Exec(t, owner, `INSERT INTO devices(user_id, name, id)
			SELECT id, 'dev', md5($1)::uuid FROM users WHERE oidc_sub = $2 ON CONFLICT DO NOTHING`, u.dev, u.user)
		h := sha256.Sum256([]byte(u.tok))
		testdb.Exec(t, owner, `INSERT INTO device_tokens(device_id, user_id, hash, scopes)
			SELECT d.id, d.user_id, $2, $3::text[] FROM devices d WHERE d.id = md5($1)::uuid`, u.dev, h[:], u.scopes)
	}
	mux := http.NewServeMux()
	e.h = ingest.New(d)
	e.h.Register(mux, httpx.NewLimiter(perMinute, time.Now))
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
	var out protocol.FilesPage
	if err := json.Unmarshal(resp.body, &out); err != nil {
		t.Fatal(err)
	}
	return out.Files
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

func TestSnapshotRevertRecorded(t *testing.T) {
	e := newEnv(t)
	for _, v := range []string{`{"v":1}`, `{"v":2}`, `{"v":1}`, `{"v":1}`} {
		c := line(v)
		e.post(t, e.tokA, protocol.IngestRequest{Files: []protocol.FileBatch{{Kind: protocol.KindSnapshot, Path: "p/s.json", Content: &c}}}, nil)
	}
	if n := e.countSnapshots(t); n != 3 {
		t.Fatalf("snapshots: %d, want 3", n)
	}
	if f := e.files(t, e.tokA); len(f) != 1 || f[0].ContentHash != protocol.HashHex([]byte(`{"v":1}`)) {
		t.Fatalf("%+v", f)
	}
}

func TestTooManyLines(t *testing.T) {
	e := newEnv(t)
	ls := make([]string, protocol.MaxLinesPerRequest+1)
	resp, _ := e.post(t, e.tokA, appendReq(0, ls...), nil)
	if resp.StatusCode != 413 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if n := e.countLines(t); n != 0 {
		t.Fatalf("stored %d", n)
	}
}

func TestConcurrencyCap(t *testing.T) {
	e := newEnv(t)
	release := e.h.HoldSlots(ingest.MaxConcurrentIngest)
	resp, _ := e.post(t, e.tokA, appendReq(0, `{"a":0}`), nil)
	if resp.StatusCode != 429 {
		t.Fatalf("full: %d", resp.StatusCode)
	}
	release()
	if resp, _ := e.post(t, e.tokA, appendReq(0, `{"a":0}`), nil); resp.StatusCode != 200 {
		t.Fatalf("after release: %d", resp.StatusCode)
	}
}

func TestOppositeOrderNoDeadlock(t *testing.T) {
	e := newEnv(t)
	mk := func(a, b string) protocol.IngestRequest {
		r := appendReq(0, `{"a":0}`)
		r.Files = append(r.Files, r.Files[0])
		r.Files[0].Path, r.Files[1].Path = a, b
		return r
	}
	for i := 0; i < 20; i++ {
		codes := make(chan int, 2)
		for _, r := range []protocol.IngestRequest{mk("p/x.jsonl", "p/y.jsonl"), mk("p/y.jsonl", "p/x.jsonl")} {
			go func() {
				resp, out := e.post(t, e.tokA, r, nil)
				if resp.StatusCode == 200 && out.Files[0].Path != r.Files[0].Path {
					resp.StatusCode = -1 // results must keep request order
				}
				codes <- resp.StatusCode
			}()
		}
		for j := 0; j < 2; j++ {
			if c := <-codes; c != 200 {
				t.Fatalf("iteration %d: %d", i, c)
			}
		}
	}
}

func TestTooManyFileEntries(t *testing.T) {
	e := newEnv(t)
	r := protocol.IngestRequest{}
	for i := 0; i <= protocol.MaxFilesPerRequest; i++ {
		c := protocol.EncodeLine([]byte(`{}`))
		r.Files = append(r.Files, protocol.FileBatch{Kind: protocol.KindSnapshot, Path: "p/" + strings.Repeat("x", 3) + string(rune('a'+i%26)) + "/" + time.Duration(i).String() + ".json", Content: &c})
	}
	if resp, _ := e.post(t, e.tokA, r, nil); resp.StatusCode != 413 {
		t.Fatalf("%d", resp.StatusCode)
	}
}

// A device that stalls its body must neither take global slots nor block other devices.
func TestStalledBodiesDoNotBlockOthers(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var pipes []*io.PipeWriter
	for i := 0; i < ingest.MaxIngestPerDevice+3; i++ {
		pr, pw := io.Pipe()
		pipes = append(pipes, pw)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, e.srv.URL+"/v1/ingest", pr)
		req.Header.Set("Authorization", "Bearer "+e.tokA)
		req.Header.Set(protocol.HeaderVersion, "1")
		req.Header.Set("Content-Encoding", "gzip")
		go func() {
			if resp, err := http.DefaultClient.Do(req); err == nil {
				_ = resp.Body.Close()
			}
		}()
	}
	defer func() {
		for _, p := range pipes {
			_ = p.Close()
		}
	}()
	time.Sleep(300 * time.Millisecond) // let the stalled requests reach the handler
	for i := 0; i < 5; i++ {
		if resp, _ := e.post(t, e.tokB, appendReq(0, `{"a":0}`), nil); resp.StatusCode != 200 {
			t.Fatalf("other device blocked: %d", resp.StatusCode)
		}
	}
}

func TestSameUserNewDeviceDedupes(t *testing.T) {
	e := newEnv(t)
	e.post(t, e.tokA, appendReq(0, `{"a":0}`, `{"a":1}`), nil)
	_, r := e.post(t, e.tokA2, appendReq(0, `{"a":0}`, `{"a":1}`), nil) // re-login / synced home: same bytes again
	if r.Files[0].Status != protocol.StatusOK || r.Files[0].EndOffset != 16 {
		t.Fatalf("%+v", r)
	}
	if n := e.countLines(t); n != 2 {
		t.Fatalf("lines %d", n)
	}
	if n := e.ownerInt(t, `SELECT count(*) FROM source_files`); n != 1 {
		t.Fatalf("source files %d", n)
	}
	fs := e.files(t, e.tokA2) // the new device sees the user's file and resumes at its end
	if len(fs) != 1 || fs[0].EndOffset != 16 || fs[0].TailOffset != 8 || fs[0].TailHash != protocol.HashHex([]byte(`{"a":1}`)) {
		t.Fatalf("%+v", fs)
	}
	if n := e.ownerInt(t, `SELECT count(*) FROM raw_lines WHERE device_id = md5('a')::uuid`); n != 2 {
		t.Fatalf("first delivering device lost: %d", n)
	}
	_, r = e.post(t, e.tokA2, appendReq(16, `{"a":2}`), nil) // and appends after it
	if r.Files[0].Status != protocol.StatusOK || e.ownerInt(t, `SELECT count(*) FROM raw_lines WHERE device_id = md5('a2')::uuid`) != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestTwoDevicesDivergingAppendsAreBothKept(t *testing.T) {
	e := newEnv(t)
	base := []string{`{"a":0}`, `{"a":1}`}
	x2, y2 := `{"a":"x2"}`, `{"a":"y2"}`
	e.post(t, e.tokA, appendReq(0, base...), nil)
	_, r := e.post(t, e.tokA2, appendReq(0, append(slices.Clone(base), x2)...), nil) // synced file: A2 appended x2
	if r.Files[0].Status != protocol.StatusOK {
		t.Fatalf("%+v", r)
	}
	end := int64(16 + len(x2) + 1)
	_, r = e.post(t, e.tokA, appendReq(16, y2), nil) // A appended something else at the same offset
	if r.Files[0].Status != protocol.StatusConflict || r.Files[0].EndOffset != end {
		t.Fatalf("%+v", r)
	}
	if n := e.countLines(t); n != 3 {
		t.Fatalf("conflicting entry stored lines: %d", n)
	}
	req := appendReq(0, append(slices.Clone(base), y2)...) // daemon fallback: device-scoped identity, from 0
	req.Files[0].Scope = protocol.ScopeDevice
	_, r = e.post(t, e.tokA, req, nil)
	if r.Files[0].Status != protocol.StatusOK || r.Files[0].EndOffset != int64(16+len(y2)+1) {
		t.Fatalf("%+v", r)
	}
	for _, want := range []string{x2, y2} {
		if n := e.ownerInt(t, `SELECT count(*) FROM raw_lines WHERE convert_from(line,'UTF8') = '`+want+`'`); n != 1 {
			t.Fatalf("%s stored %d times", want, n)
		}
	}
	if n := e.countLines(t); n != 6 { // 3 per-user + 3 device-scoped
		t.Fatalf("rows %d", n)
	}
	gotA, gotA2 := e.files(t, e.tokA), e.files(t, e.tokA2)
	scoped := 0
	for _, f := range gotA {
		if f.Scope == protocol.ScopeDevice {
			scoped++
		}
	}
	if len(gotA) != 2 || scoped != 1 || len(gotA2) != 1 || gotA2[0].Scope != "" {
		t.Fatalf("A=%+v A2=%+v", gotA, gotA2)
	}
}

func TestScopeIsRejectedForSnapshots(t *testing.T) {
	e := newEnv(t)
	c := line(`{"v":1}`)
	req := protocol.IngestRequest{Files: []protocol.FileBatch{{Kind: protocol.KindSnapshot, Path: "p/a.meta.json", Content: &c, Scope: protocol.ScopeDevice}}}
	if resp, _ := e.post(t, e.tokA, req, nil); resp.StatusCode != 400 {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestUsersNeverShareFilesOrConflict(t *testing.T) {
	e := newEnv(t)
	e.post(t, e.tokA, appendReq(0, `{"a":0}`, `{"a":1}`), nil)
	_, r := e.post(t, e.tokB, appendReq(0, `{"a":0}`, `{"a":9}`), nil) // same path+head, different user, different line at offset 8
	if r.Files[0].Status != protocol.StatusOK {
		t.Fatalf("%+v", r)
	}
	if n := e.countLines(t); n != 4 || e.ownerInt(t, `SELECT count(DISTINCT user_id) FROM source_files`) != 2 {
		t.Fatalf("users share storage: %d", n)
	}
	if fs := e.files(t, e.tokB); len(fs) != 1 || fs[0].TailHash != protocol.HashHex([]byte(`{"a":9}`)) {
		t.Fatalf("%+v", fs)
	}
}

func TestIngestNotifiesTheWorker(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c, err := pgx.Connect(ctx, e.owner)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(ctx) }()
	if _, err := c.Exec(ctx, `LISTEN braid_ingest`); err != nil {
		t.Fatal(err)
	}
	e.post(t, e.tokA, appendReq(0, `{"a":0}`), nil)
	var uid string
	if err := c.QueryRow(ctx, `SELECT id::text FROM users WHERE oidc_sub = 'a'`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	n, err := c.WaitForNotification(wctx)
	if err != nil || n.Payload != uid {
		t.Fatalf("%+v %v", n, err)
	}
}

func TestIngestAnnouncesTheSyncedDevice(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c, err := pgx.Connect(ctx, e.owner)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(ctx) }()
	if _, err := c.Exec(ctx, `LISTEN braid_synced`); err != nil {
		t.Fatal(err)
	}
	e.post(t, e.tokA, appendReq(0, `{"a":0}`), nil)
	var uid, dev string
	if err := c.QueryRow(ctx, `SELECT u.id::text, d.id::text FROM users u JOIN devices d ON d.user_id = u.id WHERE u.oidc_sub = 'a' AND d.id = md5('a')::uuid`).Scan(&uid, &dev); err != nil {
		t.Fatal(err)
	}
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	n, err := c.WaitForNotification(wctx)
	var ev live.Event
	if err != nil || json.Unmarshal([]byte(n.Payload), &ev) != nil || ev != (live.Event{UserID: uid, Type: "device_synced", DeviceID: dev}) {
		t.Fatalf("%+v %v", n, err)
	}
}

type readFlag struct{ read bool }

func (r *readFlag) Read([]byte) (int, error) { r.read = true; return 0, io.EOF }

func TestGlobalSlotIsTakenAfterTheBodyIsRead(t *testing.T) {
	e := newEnv(t)
	release := e.h.HoldSlots(ingest.MaxConcurrentIngest)
	defer release()
	mux := http.NewServeMux()
	e.h.Register(mux, httpx.NewLimiter(600, time.Now))
	body := &readFlag{}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/ingest", body)
	req.Header.Set("Authorization", "Bearer "+e.tokA)
	req.Header.Set(protocol.HeaderVersion, "1")
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 429 || !body.read { // I2: the body is read under the device slot only
		t.Fatalf("status %d, body read %v", rec.Code, body.read)
	}
}

func TestFilesArePaginatedAndTheClientResumesAll(t *testing.T) {
	e := newEnv(t)
	req := protocol.IngestRequest{}
	for i := 0; i < 2500; i++ {
		req.Files = append(req.Files, protocol.FileBatch{Kind: protocol.KindAppend, Path: fmt.Sprintf("p/%04d.jsonl", i), HeadHash: head})
	}
	if resp, _ := e.post(t, e.tokA, req, nil); resp.StatusCode != 200 {
		t.Fatalf("seed: %d", resp.StatusCode)
	}
	var page protocol.FilesPage
	resp := e.do(t, http.MethodGet, "/v1/files", e.tokA, nil, nil)
	if err := json.Unmarshal(resp.body, &page); err != nil || len(page.Files) != 500 || page.Next == "" {
		t.Fatalf("default page: %d files, next %q, err %v", len(page.Files), page.Next, err)
	}
	resp = e.do(t, http.MethodGet, "/v1/files?limit=5000", e.tokA, nil, nil)
	if err := json.Unmarshal(resp.body, &page); err != nil || len(page.Files) != 1000 {
		t.Fatalf("max page: %d files, err %v", len(page.Files), err)
	}
	if resp := e.do(t, http.MethodGet, "/v1/files?after=x", e.tokA, nil, nil); resp.StatusCode != 400 {
		t.Fatalf("bad cursor: %d", resp.StatusCode)
	}
	all, err := client.New(e.srv.URL, e.tokA, "0", nil).Files(context.Background())
	if err != nil || len(all) != 2500 {
		t.Fatalf("client got %d files, err %v", len(all), err)
	}
	seen := map[string]bool{}
	for _, f := range all {
		seen[f.Path] = true
	}
	if len(seen) != 2500 {
		t.Fatalf("duplicates across pages: %d distinct", len(seen))
	}
}

// postParts sends the chunks of one line one per request, in order given, and returns the last reply.
func (e *env) postParts(t *testing.T, tok, path string, from int64, parts []protocol.Line, order []int) protocol.IngestResponse {
	t.Helper()
	var last protocol.IngestResponse
	for _, i := range order {
		req := protocol.IngestRequest{Files: []protocol.FileBatch{{Kind: protocol.KindAppend, Path: path, HeadHash: head, FromOffset: from, Lines: []protocol.Line{parts[i]}}}}
		resp, out := e.post(t, tok, req, nil)
		if resp.StatusCode != 200 {
			t.Fatalf("part %d: %d %s", i, resp.StatusCode, resp.body)
		}
		last = out
	}
	return last
}

func seq(n int) []int {
	o := make([]int, n)
	for i := range o {
		o[i] = i
	}
	return o
}

func bigLine() []byte {
	b := make([]byte, 20<<20)
	for i := range b {
		b[i] = "abcdé\"\\\t"[i%9%8] // text with multibyte runes and escapes
	}
	return b
}

func TestLargeLineIsStoredByteExactBetweenOtherLines(t *testing.T) {
	e := newEnv(t)
	if resp, _ := e.post(t, e.tokA, appendReq(0, `{"a":0}`), nil); resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	big := bigLine()
	parts := protocol.SplitLine(big, protocol.ChunkSize)
	out := e.postParts(t, e.tokA, "p/s.jsonl", 8, parts, seq(len(parts)))
	if st := out.Files[0].Status; st != protocol.StatusOK || out.Files[0].EndOffset != 8+int64(len(big))+1 {
		t.Fatalf("last part: %+v", out.Files[0])
	}
	if resp, _ := e.post(t, e.tokA, appendReq(8+int64(len(big))+1, `{"c":1}`), nil); resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	var got []byte
	c, _ := pgx.Connect(context.Background(), e.owner)
	defer func() { _ = c.Close(context.Background()) }()
	if err := c.QueryRow(context.Background(), `SELECT line FROM raw_lines WHERE "offset" = 8`).Scan(&got); err != nil || !bytes.Equal(got, big) {
		t.Fatalf("line differs (err %v, len %d)", err, len(got))
	}
	if e.countLines(t) != 3 || e.ownerInt(t, `SELECT count(*) FROM raw_line_parts`) != 0 {
		t.Fatal("lines or leftover parts wrong")
	}
}

func TestPartsOutOfOrderAndTwice(t *testing.T) {
	e := newEnv(t)
	big := bigLine()[:3<<20+5]
	parts := protocol.SplitLine(big, protocol.ChunkSize)
	n := len(parts)
	order := []int{n - 2, 0, 0, n - 2}
	order = append(order, seq(n - 2)[1:]...)
	order = append(order, n-1, n-1) // the second delivery of the completing part is a dedupe
	out := e.postParts(t, e.tokA, "p/s.jsonl", 0, parts, order)
	if out.Files[0].Status != protocol.StatusOK || e.countLines(t) != 1 {
		t.Fatalf("%+v lines %d", out.Files[0], e.countLines(t))
	}
}

func TestTamperedPartFailsTheHashCheck(t *testing.T) {
	e := newEnv(t)
	parts := protocol.SplitLine(bigLine()[:3<<20], protocol.ChunkSize)
	last := len(parts) - 1
	e.postParts(t, e.tokA, "p/s.jsonl", 0, parts, seq(last))
	bad := "tampered"
	parts[last].Text, parts[last].B64 = &bad, nil
	req := protocol.IngestRequest{Files: []protocol.FileBatch{{Kind: protocol.KindAppend, Path: "p/s.jsonl", HeadHash: head, Lines: []protocol.Line{parts[last]}}}}
	if resp, _ := e.post(t, e.tokA, req, nil); resp.StatusCode != 400 {
		t.Fatalf("tampered: %d", resp.StatusCode)
	}
	if e.countLines(t) != 0 {
		t.Fatal("tampered line stored")
	}
}

func TestPartsAreIsolatedPerUser(t *testing.T) {
	e := newEnv(t)
	parts := protocol.SplitLine(bigLine()[:3<<20], protocol.ChunkSize)
	last := len(parts) - 1
	e.postParts(t, e.tokA, "p/s.jsonl", 0, parts, seq(last))
	out := e.postParts(t, e.tokB, "p/s.jsonl", 0, parts, []int{last}) // B cannot complete A's line
	if out.Files[0].Status != protocol.StatusPending || e.countLines(t) != 0 {
		t.Fatalf("B completed A's parts: %+v", out.Files[0])
	}
	if n := e.ownerInt(t, `SELECT count(*) FROM raw_line_parts`); n != last+1 {
		t.Fatalf("parts %d", n)
	}
}

func TestLargeSnapshotIsChunked(t *testing.T) {
	e := newEnv(t)
	content := bytes.Repeat([]byte(`{"k":"v"}`), 400000) // 3.6 MiB
	parts := protocol.SplitLine(content, protocol.ChunkSize)
	for _, p := range parts {
		c := p
		req := protocol.IngestRequest{Files: []protocol.FileBatch{{Kind: protocol.KindSnapshot, Path: "p/s/x.meta.json", Content: &c}}}
		if resp, _ := e.post(t, e.tokA, req, nil); resp.StatusCode != 200 {
			t.Fatalf("%d %s", resp.StatusCode, resp.body)
		}
	}
	var got []byte
	c, _ := pgx.Connect(context.Background(), e.owner)
	defer func() { _ = c.Close(context.Background()) }()
	if err := c.QueryRow(context.Background(), `SELECT content FROM raw_snapshots`).Scan(&got); err != nil || !bytes.Equal(got, content) {
		t.Fatalf("snapshot differs: %v", err)
	}
}

func TestChunksAreAssembledInSQLAndNeverReadIntoGo(t *testing.T) {
	for _, q := range ingest.AssemblySQL {
		if !strings.Contains(q, "string_agg(chunk") || strings.Contains(q, "SELECT chunk") {
			t.Errorf("assembly must concatenate in SQL and never select the chunks: %s", q)
		}
	}
	if len(ingest.AssemblySQL) != 3 {
		t.Fatal("verify, line and snapshot assembly statements expected")
	}
}

func partReq(path string, from int64, l protocol.Line) protocol.IngestRequest {
	return protocol.IngestRequest{Files: []protocol.FileBatch{{Kind: protocol.KindAppend, Path: path, HeadHash: head, FromOffset: from, Lines: []protocol.Line{l}}}}
}

// A leftover part with another count must neither be assembled nor change the stored line (I1).
func TestLeftoverPartOfOtherCountIsNeverAssembled(t *testing.T) {
	e := newEnv(t)
	big := bigLine()[:protocol.ChunkSize+1000]
	parts := protocol.SplitLine(big, protocol.ChunkSize)
	junk := "GARBAGE"
	stale := protocol.Line{Text: &junk, Part: &protocol.Part{Index: 2, Count: 3, TotalLen: parts[0].Part.TotalLen, SHA256: parts[0].Part.SHA256}}
	if resp, _ := e.post(t, e.tokA, partReq("p/s.jsonl", 0, stale), nil); resp.StatusCode != 200 {
		t.Fatalf("stale: %d %s", resp.StatusCode, resp.body)
	}
	e.postParts(t, e.tokA, "p/s.jsonl", 0, parts, seq(len(parts)))
	var got []byte
	c, _ := pgx.Connect(context.Background(), e.owner)
	defer func() { _ = c.Close(context.Background()) }()
	if err := c.QueryRow(context.Background(), `SELECT line FROM raw_lines`).Scan(&got); err != nil || !bytes.Equal(got, big) {
		t.Fatalf("stored line differs from its hash (err %v, len %d want %d)", err, len(got), len(big))
	}
}

// One device's new series must not wipe another device's pending parts (M4).
func TestDeviceDoesNotWipeOtherDevicesParts(t *testing.T) {
	e := newEnv(t)
	a := protocol.SplitLine(bigLine()[:3<<20], protocol.ChunkSize)
	other := protocol.SplitLine(bytes.Repeat([]byte("x"), 3<<20), protocol.ChunkSize)
	e.postParts(t, e.tokA, "p/s.jsonl", 0, a, seq(len(a)-1))
	if resp, _ := e.post(t, e.tokA2, partReq("p/s.jsonl", 0, other[0]), nil); resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	out := e.postParts(t, e.tokA, "p/s.jsonl", 0, a, []int{len(a) - 1})
	if out.Files[0].Status != protocol.StatusOK || e.countLines(t) != 1 {
		t.Fatalf("A's progress was wiped: %+v", out.Files[0])
	}
}

// Pending parts are capped per user: over the quota the answer is 413 with Retry-After (M5).
func TestPendingPartsQuota(t *testing.T) {
	defer ingest.SetMaxPendingParts(2 << 20)()
	e := newEnv(t)
	parts := protocol.SplitLine(bigLine()[:5<<20], protocol.ChunkSize)
	var last reply
	for i := 0; i < 3; i++ {
		last, _ = e.post(t, e.tokA, partReq("p/s.jsonl", 0, parts[i]), nil)
	}
	if last.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d", last.StatusCode)
	}
	if n := e.ownerInt(t, `SELECT count(*) FROM raw_line_parts`); n != 2 {
		t.Fatalf("parts %d", n)
	}
}

// Stalled bodies of two devices of one user hold no global slot, so another user still gets in (I2).
func TestStalledBodiesOfTwoDevicesDoNotBlockOtherUsers(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var pipes []*io.PipeWriter
	for _, tok := range []string{e.tokA, e.tokA, e.tokA2, e.tokA2} {
		pr, pw := io.Pipe()
		pipes = append(pipes, pw)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, e.srv.URL+"/v1/ingest", pr)
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set(protocol.HeaderVersion, "1")
		req.Header.Set("Content-Encoding", "gzip")
		go func() {
			if resp, err := http.DefaultClient.Do(req); err == nil {
				_ = resp.Body.Close()
			}
		}()
	}
	defer func() {
		for _, p := range pipes {
			_ = p.Close()
		}
	}()
	time.Sleep(300 * time.Millisecond)
	if resp, _ := e.post(t, e.tokB, appendReq(0, `{"a":0}`), nil); resp.StatusCode != 200 {
		t.Fatalf("other user blocked: %d", resp.StatusCode)
	}
}

func TestOneUserHoldsAtMostTwoGlobalSlots(t *testing.T) {
	e := newEnv(t)
	var rel []func()
	for i := 0; i < ingest.MaxGlobalPerUser; i++ {
		r, ok := e.h.TakeGlobal("u1")
		if !ok {
			t.Fatal("slot refused")
		}
		rel = append(rel, r)
	}
	if _, ok := e.h.TakeGlobal("u1"); ok {
		t.Fatal("third slot of one user granted")
	}
	if r, ok := e.h.TakeGlobal("u2"); !ok {
		t.Fatal("other user refused")
	} else {
		r()
	}
	rel[0]()
	if r, ok := e.h.TakeGlobal("u1"); !ok {
		t.Fatal("slot not freed")
	} else {
		r()
	}
	rel[1]()
}
