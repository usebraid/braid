// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

//nolint:bodyclose,errcheck,gosec,noctx // test helpers
package stats_test

import (
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	dt "github.com/usebraid/braid/internal/server/derive/derivetest"
	"github.com/usebraid/braid/internal/server/stats"
	"github.com/usebraid/braid/internal/testdb"
)

func TestSelfStatsAreTodaysAggregatesOnly(t *testing.T) {
	x := newEnv(t)
	token, _ := x.NewDevice(t, x.c, "band")
	midnight := time.Now().UTC().Truncate(24 * time.Hour)
	ts := func(d time.Duration) string { return midnight.Add(d).Format("2006-01-02T15:04:05.000Z") }
	x.dt.Put(t, x.uid, x.dev,
		dt.File{Rel: "-work-alpha/today.jsonl", Lines: []string{
			dt.Prompt(ts(time.Second), "TOPSECRET prompt"),
			dt.Reply(ts(2*time.Second), "t1", opus, dt.Usage(1000, 100, 0, 0, 0), dt.Text("TOPSECRET answer")), // USD 0.006
			dt.Turn(ts(3*time.Second), 2000)}},
		dt.File{Rel: "-work-alpha/old.jsonl", Lines: []string{
			dt.Prompt(ts(-48*time.Hour), "older"),
			dt.Reply(ts(-48*time.Hour+time.Second), "t0", opus, dt.Usage(5000, 500, 0, 0, 0), dt.Text("older"))}})
	x.dt.Derive(t, x.uid, 0)

	call := func(tok, query string, hdr map[string]string) (int, []byte) {
		req, _ := http.NewRequest("GET", x.URL+"/v1/stats/self"+query, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Braid-Protocol", "1")
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := x.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b
	}
	st, b := call(token, "?session=today", nil)
	if st != 200 {
		t.Fatalf("%d %s", st, b)
	}
	var s stats.Self
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	if s.Today.TimeZone != "UTC" || s.Today.Sessions != 1 || s.Today.AgentMs != 2000 || s.Today.Tokens != 1100 || !near(s.Today.CostUSD, 0.006) || s.Today.Failures != 0 {
		t.Fatalf("today %+v", s.Today)
	}
	if s.Session == nil || s.Session.SessionID != "today" || s.Session.AgentMs != 2000 || s.Session.TotalAgentMs != 2000 || s.Session.Tokens != 1100 {
		t.Fatalf("session %+v", s.Session)
	}
	var raw map[string]map[string]any
	_ = json.Unmarshal(b, &raw)
	var keys []string
	for k := range raw["today"] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if !slices.Equal(keys, []string{"agent_ms", "cost_usd", "failures", "from", "sessions", "time_lost_s", "time_zone", "to", "tokens"}) {
		t.Fatalf("today keys %v", keys)
	}
	if strings.Contains(string(b), "TOPSECRET") || strings.Contains(string(b), "alpha") {
		t.Fatalf("content leaked: %s", b)
	}
	if _, b := call(token, "?session=ghost", nil); !strings.Contains(string(b), `"session":null`) {
		t.Fatalf("%s", b)
	}
	if st, _ := call("braid_dev_nope", "", nil); st != 401 {
		t.Fatalf("bad token: %d", st)
	}
	if st, _ := call(token, "", map[string]string{"Braid-Protocol": "9"}); st != 426 {
		t.Fatalf("protocol: %d", st)
	}
	ingestOnly := "braid_dev_ingestonly"
	h := sha256.Sum256([]byte(ingestOnly))
	testdb.Exec(t, x.OwnerURL, `INSERT INTO device_tokens(device_id, user_id, hash, scopes) VALUES ($1, $2, $3, '{ingest:write}')`, x.dev, x.uid, h[:])
	if st, _ := call(ingestOnly, "", nil); st != 403 {
		t.Fatalf("scope: %d", st)
	}
}

func TestSelfLimitIsMostRecentUnexpired(t *testing.T) {
	x := newEnv(t)
	token, _ := x.NewDevice(t, x.c, "band")
	now := time.Now().UTC()
	ts := func(d time.Duration) string { return now.Add(d).Format("2006-01-02T15:04:05.000Z") }
	limitErr := func(at time.Duration, kind string, resets time.Duration) string {
		return dt.APIError(ts(at), "error", "rate_limit", "apiError", "usage_limit_reached", "apiErrorStatus", 429,
			"quotaLimits", map[string]any{"resetsAt": now.Add(resets).Unix(), "rateLimitType": kind})
	}
	x.dt.Put(t, x.uid, x.dev, dt.File{Rel: "-work-alpha/lim.jsonl", Lines: []string{
		dt.Prompt(ts(-4*time.Hour), "p"),
		limitErr(-3*time.Hour, "five_hour", time.Hour),            // unexpired, older
		limitErr(-2*time.Hour, "seven_day", 2*time.Hour),          // unexpired, newest unexpired: wins
		limitErr(-time.Hour, "five_hour", -time.Minute),           // newest, but expired: ignored
		dt.APIError(ts(-30*time.Minute), "error", "server_error"), // not a limit
	}})
	x.dt.Derive(t, x.uid, 0)

	get := func(q string) map[string]any {
		req, _ := http.NewRequest("GET", x.URL+"/v1/stats/self"+q, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Braid-Protocol", "1")
		resp, err := x.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var m map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&m); err != nil || resp.StatusCode != 200 {
			t.Fatalf("%d %v", resp.StatusCode, err)
		}
		return m
	}
	m := get("?session=lim")
	l, _ := m["limit"].(map[string]any)
	if l == nil || l["type"] != "seven_day" {
		t.Fatalf("limit %v", m["limit"])
	}
	at, err := time.Parse(time.RFC3339, l["resets_at"].(string))
	if err != nil || at.Unix() != now.Add(2*time.Hour).Unix() {
		t.Fatalf("resets_at %v %v", l["resets_at"], err)
	}
	if len(l) != 2 {
		t.Fatalf("limit keys %v", l)
	}
	s, _ := m["session"].(map[string]any)
	if s == nil || s["failures"] != float64(4) {
		t.Fatalf("session failures %v", m["session"])
	}
}

func TestSelfLimitNullWhenAllExpired(t *testing.T) {
	x := newEnv(t)
	token, _ := x.NewDevice(t, x.c, "band")
	now := time.Now().UTC()
	x.dt.Put(t, x.uid, x.dev, dt.File{Rel: "-work-alpha/old.jsonl", Lines: []string{
		dt.Prompt(now.Add(-3*time.Hour).Format("2006-01-02T15:04:05.000Z"), "p"),
		dt.APIError(now.Add(-2*time.Hour).Format("2006-01-02T15:04:05.000Z"), "error", "rate_limit", "apiError", "usage_limit_reached", "apiErrorStatus", 429,
			"quotaLimits", map[string]any{"resetsAt": now.Add(-time.Hour).Unix(), "rateLimitType": "five_hour"}),
	}})
	x.dt.Derive(t, x.uid, 0)
	req, _ := http.NewRequest("GET", x.URL+"/v1/stats/self", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Braid-Protocol", "1")
	resp, err := x.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), `"limit":null`) {
		t.Fatalf("%s", b)
	}
}
