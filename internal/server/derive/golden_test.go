// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package derive_test

import (
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	dt "github.com/usebraid/braid/internal/server/derive/derivetest"
)

var update = flag.Bool("update", false, "rewrite testdata golden files")

func TestGoldenFullScenario(t *testing.T) {
	e, u, d := setup(t)
	e.Put(t, u, d, dt.Scenario()...)
	e.Derive(t, u, 0)

	checks := map[string]string{ // independent of the golden file: figures worked out by hand from the price table (fast mode and the unknown fable-5-2 are estimated, not zero)
		`SELECT count(*), count(*) FILTER (WHERE price_unknown), count(*) FILTER (WHERE price_estimated), round(sum(cost_usd)*1e6)::bigint FROM usage`: "10|0|2|40988",
		`SELECT sum(input_tokens+output_tokens+cache_read_tokens+cache_write_5m_tokens+cache_write_1h_tokens)::bigint FROM usage`:                      "5474",
		`SELECT round(cost_usd*1e6)::bigint FROM usage WHERE message_id='g-m1'`:                                                                        "10500", // 1000*4+200*5+300*8+500*0.2+150*20 (final streamed output wins)
		`SELECT round(cost_usd*1e6)::bigint FROM usage WHERE message_id='g-m5'`:                                                                        "20000", // two web searches
		`SELECT title || '|' || line_count || '|' || project_path FROM sessions`:                                                                       "Golden custom|35|/work/alpha",
		`SELECT string_agg(agent_id || '=' || duration_ms || '/' || duration_estimated, ',' ORDER BY agent_id) FROM agents`:                            "a1=3000/false,a2=8000/false,a3=8000/false,main=940000/false",
		`SELECT string_agg(agent_id || '>' || coalesce(parent_agent_id, '-'), ',' ORDER BY agent_id) FROM agents`:                                      "a1>main,a2>main,a3>a1,main>-",
		`SELECT string_agg(category || '=' || n, ',' ORDER BY category) FROM (SELECT category, count(*) n FROM failures GROUP BY 1) x`:                 "network=1,other_api_error=1,rate_limit=1,refusal=1,server_error=1,tool_error=1,usage_limit=1",
		`SELECT string_agg(agent_id || ':' || seq || ':' || skill, ',' ORDER BY agent_id, seq) FROM skill_phases`:                                      "a1:1:x:debug,main:1:superpowers:brainstorming,main:2:superpowers:writing-plans",
		`SELECT errors FROM derive_errors`: "1",
		`SELECT count(*) FROM search_docs`: "5",
	}
	for q, want := range checks {
		if got := e.Row(t, q); got != want {
			t.Errorf("%s\n got  %s\n want %s", q, got, want)
		}
	}

	got := e.Dump(t, u, map[string]string{u: "USER", d: "DEVICE"})
	golden := filepath.Join("testdata", "golden_full.txt")
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden) //nolint:gosec // fixed path under testdata
	if err != nil {
		t.Fatalf("%v (run: go test ./internal/server/derive -run Golden -update)", err)
	}
	if string(want) != got {
		t.Fatalf("derived tables differ from %s (rerun with -update only after checking the diff):\n%s", golden, got)
	}
}

func TestDerivationIsIdempotentChunkIndependentAndEqualsReindex(t *testing.T) {
	e := dt.New(t)
	files := dt.Scenario()
	repl := func(u, d string) map[string]string { return map[string]string{u: "USER", d: "DEVICE"} }
	newUser := func(sub string) (string, string) {
		u := e.User(t, sub)
		return u, e.Device(t, u, "laptop")
	}
	u0, d0 := newUser("base")
	e.Put(t, u0, d0, files...)
	e.Derive(t, u0, 0)
	base := e.Dump(t, u0, repl(u0, d0))
	if e.Derive(t, u0, 0) != 0 || e.Dump(t, u0, repl(u0, d0)) != base {
		t.Fatal("a second run changed something")
	}
	e.Reset(t, u0)
	e.Derive(t, u0, 0)
	if e.Dump(t, u0, repl(u0, d0)) != base {
		t.Fatal("reindex differs from the incremental result")
	}
	rnd := rand.New(rand.NewPCG(7, 11)) //nolint:gosec // deterministic shuffle of test files
	for i, batch := range []int{1, 2, 3, 7, 1000} {
		u, d := newUser(fmt.Sprintf("chunk%d", i))
		order := rnd.Perm(len(files))
		for _, j := range order {
			e.Put(t, u, d, files[j])
		}
		e.Derive(t, u, batch)
		if got := e.Dump(t, u, repl(u, d)); got != base {
			t.Fatalf("batch %d, file order %v differs from the baseline", batch, order)
		}
	}
	u, d := newUser("incremental") // raw data arriving in two parts, derived in between
	main := files[0]
	half := len(main.Lines) / 2
	e.Put(t, u, d, dt.File{Rel: main.Rel, Lines: main.Lines[:half]})
	e.Derive(t, u, 0)
	e.Put(t, u, d, dt.File{Rel: main.Rel, Lines: main.Lines[half:]})
	e.Put(t, u, d, files[1:]...)
	e.Derive(t, u, 0)
	if got := e.Dump(t, u, repl(u, d)); got != base {
		t.Fatal("incremental result differs from the full run")
	}
}

func TestScenarioIsExported(t *testing.T) {
	files := dt.Scenario()
	if len(files) != 8 || files[0].Rel != "-work-alpha/g1.jsonl" {
		t.Fatalf("%d files, first %s", len(files), files[0].Rel)
	}
}
