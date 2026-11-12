// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package derive_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/usebraid/braid/internal/server/derive"
	dt "github.com/usebraid/braid/internal/server/derive/derivetest"
	"github.com/usebraid/braid/internal/testdb"
)

func startWorker(t *testing.T, e *dt.Env, sweep time.Duration) *derive.Worker {
	t.Helper()
	w := &derive.Worker{D: &derive.Deriver{DB: e.DB}, DB: e.DB, Sweep: sweep, Debounce: 10 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	select {
	case <-w.Ready():
	case <-time.After(10 * time.Second):
		t.Fatal("worker not ready")
	}
	return w
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(25 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestNotificationDrivesDerivationAndAnnouncesIt(t *testing.T) {
	ctx := context.Background()
	e, u, d := setup(t)
	c, err := pgx.Connect(ctx, e.Owner)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(ctx) }()
	if _, err := c.Exec(ctx, `LISTEN braid_derived`); err != nil {
		t.Fatal(err)
	}
	startWorker(t, e, time.Hour) // the sweep never fires: only the notification can explain the result
	e.Put(t, u, d, dt.File{Rel: "-work-alpha/s1.jsonl", Lines: []string{dt.Prompt(dt.At(0), "hello")}})
	testdb.Exec(t, e.Owner, `SELECT pg_notify('braid_ingest', $1)`, u)
	eventually(t, "session", func() bool { return e.N(t, `SELECT count(*) FROM sessions`) == 1 })
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	n, err := c.WaitForNotification(wctx)
	if err != nil || n.Payload != u {
		t.Fatalf("braid_derived: %+v %v", n, err)
	}
}

func TestSweepCatchesMissedNotifications(t *testing.T) {
	e, u, d := setup(t)
	startWorker(t, e, 100*time.Millisecond)
	e.Put(t, u, d, dt.File{Rel: "-work-alpha/s1.jsonl", Lines: []string{dt.Prompt(dt.At(0), "hello")}}) // no NOTIFY
	eventually(t, "session", func() bool { return e.N(t, `SELECT count(*) FROM sessions`) == 1 })
}

func TestParserVersionBumpRebuilds(t *testing.T) {
	e, u, d := setup(t)
	e.Put(t, u, d, dt.File{Rel: "-work-alpha/s1.jsonl", Lines: []string{dt.Prompt(dt.At(0), "p"), dt.Line("ai-title", "", "aiTitle", "Fresh")}})
	e.Derive(t, u, 0)
	testdb.Exec(t, e.Owner, `INSERT INTO derive_state(user_id, parser_version) VALUES ($1, $2)`, u, derive.ParserVersion-1)
	testdb.Exec(t, e.Owner, `UPDATE sessions SET ai_title = 'stale'`)
	startWorker(t, e, time.Hour)
	eventually(t, "rebuild", func() bool {
		return e.N(t, `SELECT count(*) FROM derive_state WHERE parser_version = $1 AND NOT rebuilding`, derive.ParserVersion) == 1 &&
			e.N(t, `SELECT count(*) FROM sessions WHERE ai_title = 'Fresh'`) == 1
	})
}

func TestReindexAllRebuildsFromRaw(t *testing.T) {
	ctx := context.Background()
	e, u, d := setup(t)
	e.Put(t, u, d, dt.File{Rel: "-work-alpha/s1.jsonl", Lines: []string{dt.Prompt(dt.At(0), "p"), dt.Reply(dt.At(1), "m1", opus, dt.Usage(1, 1, 0, 0, 0))}})
	e.Derive(t, u, 0)
	before := e.Dump(t, u, map[string]string{u: "U", d: "D"})
	testdb.Exec(t, e.Owner, `DELETE FROM sessions`)
	testdb.Exec(t, e.Owner, `DELETE FROM usage`)
	w := &derive.Worker{D: &derive.Deriver{DB: e.DB}, DB: e.DB}
	if err := w.ReindexAll(ctx); err != nil {
		t.Fatal(err)
	}
	if after := e.Dump(t, u, map[string]string{u: "U", d: "D"}); after != before {
		t.Fatalf("reindex differs:\n%s\n---\n%s", before, after)
	}
}
