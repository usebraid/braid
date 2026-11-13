// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestEncodeLineRoundTrip(t *testing.T) {
	cases := [][]byte{
		[]byte(`{"type":"user"}`),
		[]byte("ümlaut ✓"),
		{0xff, 0xfe, 0x00, 'a'},
		{},
	}
	for _, raw := range cases {
		l := EncodeLine(raw)
		if (l.Text != nil) == (l.B64 != nil) {
			t.Fatalf("exactly one of Text/B64 must be set for %q", raw)
		}
		j, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		var back Line
		if err := json.Unmarshal(j, &back); err != nil {
			t.Fatal(err)
		}
		got, err := back.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, raw) {
			t.Fatalf("round trip %q → %q", raw, got)
		}
	}
	if l := EncodeLine([]byte{0xff}); l.Text != nil {
		t.Fatal("invalid UTF-8 must use B64")
	}
	if l := EncodeLine([]byte("ok")); l.Text == nil {
		t.Fatal("valid UTF-8 must use Text")
	}
}

func TestLineBytesRejectsBothOrNeither(t *testing.T) {
	s := "x"
	if _, err := (Line{}).Bytes(); err == nil {
		t.Fatal("neither set must fail")
	}
	if _, err := (Line{Text: &s, B64: []byte("x")}).Bytes(); err == nil {
		t.Fatal("both set must fail")
	}
}

func TestEndOffset(t *testing.T) {
	if got := EndOffset(10, [][]byte{[]byte("ab"), []byte(""), []byte("cde")}); got != 10+3+1+4 {
		t.Fatalf("got %d", got)
	}
	if got := EndOffset(7, nil); got != 7 {
		t.Fatalf("got %d", got)
	}
}

func TestValidPath(t *testing.T) {
	ok := []struct {
		p string
		k Kind
	}{
		{"-Users-x-proj/abc.jsonl", KindAppend},
		{strings.Repeat("a", MaxPathLen-6) + ".jsonl", KindAppend},
		{"-Users-x-proj/abc/subagents/agent-a1.meta.json", KindSnapshot},
	}
	for _, c := range ok {
		if err := ValidPath(c.p, c.k); err != nil {
			t.Errorf("%q: %v", c.p, err)
		}
	}
	bad := []struct {
		p string
		k Kind
	}{
		{"/etc/passwd.jsonl", KindAppend},
		{"../x.jsonl", KindAppend},
		{"a/../../x.jsonl", KindAppend},
		{"a/./x.jsonl", KindAppend},
		{"a//x.jsonl", KindAppend},
		{`a\x.jsonl`, KindAppend},
		{"a/x\x00.jsonl", KindAppend},
		{"a/x.json", KindAppend},
		{"a/x.jsonl", KindSnapshot},
		{"", KindAppend},
		{strings.Repeat("a", MaxPathLen) + ".jsonl", KindAppend},
		{"a/x.txt", KindAppend},
		{"a/x\x01.jsonl", KindAppend},
		{strings.Repeat("a", MaxPathLen-6+1) + ".jsonl", KindAppend},
	}
	for _, c := range bad {
		if err := ValidPath(c.p, c.k); err == nil {
			t.Errorf("%q must be rejected", c.p)
		}
	}
}

func TestRequestValidate(t *testing.T) {
	h := strings.Repeat("a", 64)
	txt := "{}"
	good := IngestRequest{Files: []FileBatch{
		{Kind: KindAppend, Path: "p/a.jsonl", HeadHash: h, FromOffset: 0, Lines: []Line{{Text: &txt}}},
		{Kind: KindSnapshot, Path: "p/a.json", Content: &Line{Text: &txt}},
	}}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	bads := []FileBatch{
		{Kind: "other", Path: "p/a.jsonl", HeadHash: h},
		{Kind: KindAppend, Path: "p/a.jsonl", HeadHash: "short"},
		{Kind: KindAppend, Path: "p/a.jsonl", HeadHash: h, FromOffset: -1},
		{Kind: KindAppend, Path: "p/a.jsonl", HeadHash: h, Content: &Line{Text: &txt}},
		{Kind: KindSnapshot, Path: "p/a.json"},
		{Kind: KindSnapshot, Path: "p/a.json", HeadHash: h, Content: &Line{Text: &txt}},
		{Kind: KindSnapshot, Path: "p/a.json", Content: &Line{Text: &txt}, Lines: []Line{{Text: &txt}}},
		{Kind: KindAppend, Path: "p/a.jsonl", HeadHash: h, Lines: []Line{{}}},
	}
	nl := "a\nb"
	bads = append(bads,
		FileBatch{Kind: KindAppend, Path: "p/a.jsonl", HeadHash: h, Lines: []Line{{Text: &nl}}},
		FileBatch{Kind: KindAppend, Path: "p/a.jsonl", HeadHash: h, Lines: []Line{{B64: []byte{0xff, '\n'}}}},
	)
	for i, b := range bads {
		if err := (IngestRequest{Files: []FileBatch{b}}).Validate(); err == nil {
			t.Errorf("bad[%d] accepted", i)
		}
	}
}

func TestNormalizeUserCode(t *testing.T) {
	got, err := NormalizeUserCode(" bcdf-ghjk ")
	if err != nil || got != "BCDFGHJK" {
		t.Fatalf("%q %v", got, err)
	}
	for _, s := range []string{"BCDF-GHJ", "BCDF-GHJA", "BCDF-GHJK1", ""} {
		if _, err := NormalizeUserCode(s); err == nil {
			t.Errorf("%q accepted", s)
		}
	}
}

func TestDeviceCodeRequestValidate(t *testing.T) {
	ok := DeviceCodeRequest{Hostname: "mac", OS: "darwin", Arch: "arm64", OSUser: "jan", DaemonVersion: "0.1.0"}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := ok
	bad.Hostname = ""
	if bad.Validate() == nil {
		t.Error("empty hostname accepted")
	}
	bad = ok
	bad.OSUser = "a\x1b[31m"
	if bad.Validate() == nil {
		t.Error("control char accepted")
	}
	bad = ok
	bad.OS = strings.Repeat("x", 129)
	if bad.Validate() == nil {
		t.Error("long field accepted")
	}
}

func TestDeviceFieldsRejectInvisibleAndBidi(t *testing.T) {
	for _, v := range []string{"a\u202eb", "a\u2066b", "a\u2069b", "a\u200bb", "a\u200eb", "\ufeffa", "a\u2028b", "a\u2029b", "a\U000e0041b", "\u00a0", "   ", "a\x00b"} {
		bad := DeviceCodeRequest{Hostname: v, OS: "darwin", Arch: "arm64", OSUser: "jan", DaemonVersion: "0.1.0"}
		if bad.Validate() == nil {
			t.Errorf("hostname %q accepted", v)
		}
		bad.Hostname, bad.OSUser = "mac", v
		if bad.Validate() == nil {
			t.Errorf("os_user %q accepted", v)
		}
		if ValidText(v, 64) {
			t.Errorf("ValidText(%q)", v)
		}
	}
	if !ValidText("Jan's Mac \u00e9\u65e5\u672c", 64) || ValidText(strings.Repeat("x", 65), 64) {
		t.Error("ValidText limits")
	}
}

func TestValidateLineCap(t *testing.T) {
	txt := "{}"
	mk := func(n int) IngestRequest {
		return IngestRequest{Files: []FileBatch{{Kind: KindAppend, Path: "p/a.jsonl", HeadHash: strings.Repeat("a", 64), Lines: make([]Line, n)}}}
	}
	ok := mk(MaxLinesPerRequest)
	for i := range ok.Files[0].Lines {
		ok.Files[0].Lines[i] = Line{Text: &txt}
	}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := mk(MaxLinesPerRequest + 1).Validate(); !errors.Is(err, ErrTooManyLines) {
		t.Fatalf("got %v", err)
	}
}

func TestScopeValidation(t *testing.T) {
	h := HashHex([]byte("h"))
	c := EncodeLine([]byte("{}"))
	cases := map[string]struct {
		fb      FileBatch
		wantErr bool
	}{
		"device append":  {FileBatch{Kind: KindAppend, Path: "p/a.jsonl", HeadHash: h, Scope: ScopeDevice}, false},
		"user append":    {FileBatch{Kind: KindAppend, Path: "p/a.jsonl", HeadHash: h}, false},
		"unknown scope":  {FileBatch{Kind: KindAppend, Path: "p/a.jsonl", HeadHash: h, Scope: "team"}, true},
		"snapshot scope": {FileBatch{Kind: KindSnapshot, Path: "p/a.json", Content: &c, Scope: ScopeDevice}, true},
	}
	for name, tc := range cases {
		if err := (IngestRequest{Files: []FileBatch{tc.fb}}).Validate(); (err != nil) != tc.wantErr {
			t.Errorf("%s: err=%v", name, err)
		}
	}
}

func TestSplitLineKeepsRunesWholeAndRoundTrips(t *testing.T) {
	raw := []byte(strings.Repeat("aé€😀", 700000)) // multibyte runes straddle every chunk edge
	parts := SplitLine(raw, ChunkSize)
	var back []byte
	for i, p := range parts {
		b, _ := p.Bytes()
		if p.Text == nil || !utf8.Valid(b) || len(b) > ChunkSize || p.Part.Index != i || p.Part.Count != len(parts) {
			t.Fatalf("chunk %d: text %v len %d", i, p.Text != nil, len(b))
		}
		back = append(back, b...)
	}
	if !bytes.Equal(back, raw) || parts[0].Part.TotalLen != int64(len(raw)) || parts[0].Part.SHA256 != HashHex(raw) {
		t.Fatal("round trip differs")
	}
	req := IngestRequest{Files: []FileBatch{{Kind: KindAppend, Path: "p/a.jsonl", HeadHash: HashHex(nil), Lines: parts[:1]}}}
	if err := req.Validate(); err != nil {
		t.Fatal(err)
	}
	req.Files[0].Lines = parts[:2] // a part must be the only line
	if req.Validate() == nil {
		t.Fatal("two lines with a part accepted")
	}
}
