// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package derive_test

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	dt "github.com/usebraid/braid/internal/server/derive/derivetest"
)

func bulk(n, from int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("p%d", from+i)
		if i%5 == 0 {
			out = append(out, dt.Prompt(dt.At(from+i), "prompt "+id))
		} else {
			out = append(out, dt.Reply(dt.At(from+i), id, opus, dt.Usage(100, 10, 0, 0, 0), dt.Text(strings.Repeat("x", 900))))
		}
	}
	return out
}

func TestIncrementalBatchIsFast(t *testing.T) { // spec §9: a new batch derives within 2 s
	e, u, d := setup(t)
	e.Put(t, u, d, dt.File{Rel: "-work-alpha/big.jsonl", Lines: bulk(20000, 0)})
	e.Derive(t, u, 0)
	e.Put(t, u, d, dt.File{Rel: "-work-alpha/big.jsonl", Lines: bulk(500, 20000)})
	start := time.Now()
	if n := e.Derive(t, u, 0); n != 500 {
		t.Fatalf("rows %d", n)
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("incremental batch took %v", el)
	}
}

func TestDeriveThroughput(t *testing.T) { // spec §9: 1 GB within 10 minutes on 2 cores; run on a 2-core machine with BRAID_PERF=1
	if os.Getenv("BRAID_PERF") != "1" {
		t.Skip("set BRAID_PERF=1")
	}
	mb := 256
	if v, err := strconv.Atoi(os.Getenv("BRAID_PERF_MB")); err == nil && v > 0 {
		mb = v
	}
	e, u, d := setup(t)
	lines := mb * 1024 // ~1 KiB per line
	for from := 0; from < lines; from += 20000 {
		e.Put(t, u, d, dt.File{Rel: "-work-alpha/perf.jsonl", Lines: bulk(min(20000, lines-from), from)})
	}
	start := time.Now()
	e.Derive(t, u, 0)
	el := time.Since(start)
	perGB := time.Duration(float64(el) * 1024 / float64(mb))
	t.Logf("%d MB in %v, extrapolated %v per GB", mb, el, perGB)
	if perGB > 10*time.Minute {
		t.Fatalf("extrapolated %v per GB exceeds 10 minutes", perGB)
	}
}
