// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package derive_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/usebraid/braid/internal/server/derive"
	dt "github.com/usebraid/braid/internal/server/derive/derivetest"
)

func TestSearchIndexStoresNoText(t *testing.T) {
	e, u, d := setup(t)
	e.Put(t, u, d, dt.File{Rel: "-work-alpha/s1.jsonl", Lines: []string{
		dt.Prompt(dt.At(0), "Please fix the flaky zebra test"),
		dt.Reply(dt.At(1), "m1", opus, dt.Usage(1, 1, 0, 0, 0), dt.Text("I will inspect the quokka module"),
			dt.Tool("t1", "Edit", map[string]any{"file_path": "/work/alpha/internal/handler.go", "old_string": "SECRET-OLD", "new_string": "SECRET-NEW"})),
		dt.Result(dt.At(2), "t1", false, map[string]any{}), // nothing searchable
	}})
	e.Derive(t, u, 0)
	for q, want := range map[string]int{
		`tsv @@ plainto_tsquery('simple','zebra') AND role='user'`:       1,
		`tsv @@ plainto_tsquery('simple','quokka') AND role='assistant'`: 1,
		`tsv @@ plainto_tsquery('simple','edit')`:                        1, // tool names are searchable
		`tsv @@ plainto_tsquery('simple','secret')`:                      0, // tool input content is not
		`idents ILIKE '%handl%'`:                                         1, // partial identifier via pg_trgm
		`idents ILIKE '%zebra%'`:                                         0,
		`true`:                                                           2,
	} {
		if got := e.N(t, `SELECT count(*) FROM search_docs WHERE `+q); got != want {
			t.Errorf("%s: %d want %d", q, got, want)
		}
	}
	// data minimisation: no column holds a copy of the text
	if got := e.Row(t, `SELECT string_agg(column_name, ',' ORDER BY column_name) FROM information_schema.columns WHERE table_name='search_docs'`); got != "agent_id,at,idents,offset,role,session_id,source_file_id,tsv,user_id" {
		t.Fatalf("columns: %s", got)
	}
	if e.N(t, `SELECT count(*) FROM search_docs WHERE tsv::text ILIKE '%SECRET%'`) != 0 {
		t.Fatal("tool input leaked into the index")
	}
}

func TestSnippet(t *testing.T) {
	long := strings.Repeat("lorem ", 100) + "needle" + strings.Repeat(" ipsum", 100)
	s := derive.Snippet([]byte(dt.Prompt(dt.At(0), long)), "user", "Needle")
	if !strings.Contains(s, "needle") || utf8.RuneCountInString(s) > 164 || strings.ContainsAny(s, "{}") || !strings.HasPrefix(s, "…") || !strings.HasSuffix(s, "…") {
		t.Fatalf("%q", s)
	}
	asst := dt.Reply(dt.At(1), "m1", opus, dt.Usage(1, 1, 0, 0, 0), dt.Text("nothing here"),
		dt.Tool("t1", "Read", map[string]any{"file_path": "/work/alpha/internal/handler.go"}))
	if s := derive.Snippet([]byte(asst), "assistant", "handler"); !strings.Contains(s, "handler.go") || !strings.HasPrefix(s, "Read ") {
		t.Fatalf("%q", s)
	}
	if s := derive.Snippet([]byte(asst), "assistant", "absent"); s != "nothing here" {
		t.Fatalf("no-match fallback: %q", s)
	}
	for _, bad := range []string{"", "not json", `{"type":"mode"}`} {
		if s := derive.Snippet([]byte(bad), "user", "x"); s != "" {
			t.Errorf("%q -> %q", bad, s)
		}
	}
}
