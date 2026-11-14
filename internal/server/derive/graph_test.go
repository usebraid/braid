// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package derive_test

import (
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"

	dt "github.com/usebraid/braid/internal/server/derive/derivetest"
)

const eventsQ = `SELECT coalesce(string_agg(kind || '@' || to_char(at AT TIME ZONE 'UTC', 'HH24:MI') || '/' || coalesce(model, '-'), ',' ORDER BY at, kind), '') FROM session_events`

const orderQ = `SELECT coalesce(string_agg(agent_id || '@' || to_char(ts AT TIME ZONE 'UTC', 'HH24:MI:SS'), ',' ORDER BY raw_line_id), '') FROM line_order`

var gu = dt.Usage(1, 1, 0, 0, 0)

// events derives the lines of one main transcript in the given batch size and returns the session events.
func events(t *testing.T, batch int, lines ...string) string {
	t.Helper()
	e, u, d := setup(t)
	e.Put(t, u, d, dt.File{Rel: "-work-alpha/s1.jsonl", Lines: lines})
	e.Derive(t, u, batch)
	return e.Row(t, eventsQ)
}

func TestEventsStartOnly(t *testing.T) {
	for _, batch := range []int{0, 1} {
		if got := events(t, batch, dt.Prompt(dt.At(0), "a"), dt.Reply(dt.At(5), "m1", opus, gu), dt.Prompt(dt.At(1200), "b")); got != "start@10:00/claude-opus-5-5" {
			t.Errorf("batch %d: %s", batch, got)
		}
	}
}

func TestEventsResumeAfterIdle(t *testing.T) {
	for _, batch := range []int{0, 1} {
		got := events(t, batch, dt.Prompt(dt.At(0), "a"), dt.Reply(dt.At(60), "m1", opus, gu), dt.Prompt(dt.At(60+45*60), "b"), dt.Reply(dt.At(60+45*60+5), "m2", sonnet, gu))
		if want := "start@10:00/claude-opus-5-5,resume@10:46/claude-sonnet-5-5"; got != want {
			t.Errorf("batch %d: %s want %s", batch, got, want)
		}
	}
}

func TestEventsNoResumeAfterReportedToolRun(t *testing.T) {
	for _, batch := range []int{0, 1} {
		got := events(t, batch, dt.Prompt(dt.At(0), "a"), dt.Reply(dt.At(10), "m1", opus, gu, dt.Tool("t1", "Bash", map[string]any{})),
			dt.Result(dt.At(10+2400), "t1", false, map[string]any{"durationMs": 2_400_000}), dt.Prompt(dt.At(10+2400+60), "b"))
		if want := "start@10:00/claude-opus-5-5"; got != want {
			t.Errorf("batch %d: %s want %s", batch, got, want)
		}
	}
}

func TestEventsResumeOnDeviceSwitch(t *testing.T) {
	for _, batch := range []int{0, 1} {
		e, u, a := setup(t)
		b := e.Device(t, u, "desktop")
		const rel = "-work-alpha/s1.jsonl"
		e.Put(t, u, a, dt.File{Rel: rel, Lines: []string{dt.Prompt(dt.At(0), "a"), dt.Reply(dt.At(10), "m1", opus, gu)}})
		e.Put(t, u, b, dt.File{Rel: rel, Lines: []string{dt.Prompt(dt.At(310), "b"), dt.Reply(dt.At(320), "m2", opus, gu)}})
		e.Derive(t, u, batch)
		if got, want := e.Row(t, `SELECT string_agg(kind || '=' || (device_id = $1)::text, ',' ORDER BY at) FROM session_events`, b), "start=false,resume=true"; got != want {
			t.Errorf("batch %d: %s want %s", batch, got, want)
		}
	}
}

func TestEventsModelAcrossBatches(t *testing.T) {
	e, u, d := setup(t)
	const rel = "-work-alpha/s1.jsonl"
	e.Put(t, u, d, dt.File{Rel: rel, Lines: []string{dt.Prompt(dt.At(0), "a"), dt.Reply(dt.At(5), "m1", opus, gu), dt.Prompt(dt.At(4000), "b")}})
	e.Derive(t, u, 0)
	if got, want := e.Row(t, eventsQ), "start@10:00/claude-opus-5-5,resume@11:06/-"; got != want {
		t.Fatalf("before the reply: %s want %s", got, want)
	}
	e.Put(t, u, d, dt.File{Rel: rel, Lines: []string{dt.Reply(dt.At(4010), "m2", sonnet, gu)}})
	e.Derive(t, u, 0)
	if got, want := e.Row(t, eventsQ), "start@10:00/claude-opus-5-5,resume@11:06/claude-sonnet-5-5"; got != want {
		t.Fatalf("after the reply: %s want %s", got, want)
	}
}

func TestEventsStartMovedEarlierByOutOfOrderLine(t *testing.T) {
	lines := []string{dt.Prompt(dt.At(300), "a"), dt.Reply(dt.At(310), "m1", opus, gu), dt.Prompt(dt.At(0), "b"), dt.Reply(dt.At(10), "m2", sonnet, gu)}
	for _, batch := range []int{0, 1, 2} {
		if got, want := events(t, batch, lines...), "start@10:00/claude-sonnet-5-5"; got != want {
			t.Errorf("batch %d: %s want %s", batch, got, want)
		}
	}
}

func TestLineOrderMissingTimestamp(t *testing.T) {
	lines := []string{dt.Line("system", ""), dt.Prompt(dt.At(5), "a"), dt.Line("system", ""), dt.Reply(dt.At(9), "m1", opus, gu)}
	for _, batch := range []int{0, 1, 3} {
		e, u, d := setup(t)
		e.Put(t, u, d, dt.File{Rel: "-work-alpha/s1.jsonl", Lines: lines})
		e.Derive(t, u, batch)
		if got, want := e.Row(t, orderQ), "main@10:00:05,main@10:00:05,main@10:00:05,main@10:00:09"; got != want {
			t.Errorf("batch %d: %s want %s", batch, got, want)
		}
	}
}

func TestGraphTablesEqualReindex(t *testing.T) {
	rnd := rand.New(rand.NewPCG(9, 4)) //nolint:gosec // deterministic test data
	var random []string
	for at, i := 0, 0; i < 80; i++ {
		switch rnd.IntN(3) {
		case 0:
			random = append(random, dt.Prompt(dt.At(at), "p"))
		case 1:
			random = append(random, dt.Line("system", ""))
		default:
			random = append(random, dt.Reply(dt.At(at), "m"+strconv.Itoa(i), opus, gu, dt.Tool("t"+strconv.Itoa(i), "Bash", map[string]any{})))
		}
		at += []int{5, 90, 1700, 2000, 9000}[rnd.IntN(5)]
		if rnd.IntN(6) == 0 { // a line stamped earlier than its predecessors
			at -= 3000
		}
	}
	e := dt.New(t)
	for name, files := range map[string][]dt.File{"scenario": dt.Scenario(), "random": {{Rel: "-work-alpha/r1.jsonl", Lines: random}}} {
		var base string
		for i, batch := range []int{0, 1, 3, 1000, -1} {
			u := e.User(t, name+strconv.Itoa(i))
			d := e.Device(t, u, "laptop")
			e.Put(t, u, d, files...)
			if batch < 0 { // a reindex
				e.Derive(t, u, 0)
				e.Reset(t, u)
				batch = 0
			}
			e.Derive(t, u, batch)
			got := e.Row(t, `SELECT (SELECT coalesce(string_agg(agent_id || session_id || ts::text, ',' ORDER BY raw_line_id), '') FROM line_order WHERE user_id = $1)
			  || '#' || (SELECT coalesce(string_agg(kind || at::text || coalesce(model, '-') || coalesce(device_id = $2, false)::text || "offset", ',' ORDER BY at, kind, "offset"), '') FROM session_events WHERE user_id = $1)`, u, d)
			if base == "" {
				base = got
				if !strings.Contains(got, "#start") {
					t.Fatalf("%s: degenerate fixture %s", name, got)
				}
				if name == "random" && !strings.Contains(got, "resume") {
					t.Fatalf("%s: no resume in the fixture", name)
				}
			} else if got != base {
				t.Fatalf("%s: batch %d differs from the reference", name, batch)
			}
		}
	}
}

func TestEventsSkipSyntheticModel(t *testing.T) {
	for _, batch := range []int{0, 1} {
		got := events(t, batch, dt.Prompt(dt.At(0), "a"), dt.Reply(dt.At(5), "m0", "<synthetic>", gu, dt.Text("API error")), dt.Reply(dt.At(9), "m1", opus, gu))
		if want := "start@10:00/claude-opus-5-5"; got != want {
			t.Errorf("batch %d: %s want %s", batch, got, want)
		}
	}
}
