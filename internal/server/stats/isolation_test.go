// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

//nolint:bodyclose,errcheck,gosec,noctx // test helpers
package stats_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/usebraid/braid/internal/server/api"
	"github.com/usebraid/braid/internal/server/stats"
)

func TestUserBSeesNothingOfUserA(t *testing.T) {
	x := newEnv(t)
	seed(t, x)
	cB := x.Login(t, "u2")
	var meB api.Me
	x.getAs(t, cB, "/api/v1/me", &meB)
	tokB, _ := x.NewDevice(t, cB, "b-device")

	var ov stats.Overview
	if st := x.getAs(t, cB, "/api/v1/stats/overview?"+rng, &ov); st != 200 || ov.KPI.Sessions != 0 || ov.KPI.Tokens != 0 || ov.KPI.CostUSD != 0 || ov.KPI.Failures != 0 || len(ov.PerDay) != 0 || len(ov.PerProject) != 0 {
		t.Fatalf("overview leaks: %d %+v", st, ov)
	}
	var sp stats.SessionPage
	if x.getAs(t, cB, "/api/v1/stats/sessions?"+rng, &sp); len(sp.Items) != 0 {
		t.Fatalf("sessions leak: %+v", sp)
	}
	for _, p := range []string{"/api/v1/stats/sessions/s1", "/api/v1/stats/sessions/s2/lines?agent=main", "/api/v1/stats/sessions/s1/lines?agent=a1"} {
		if st := x.getAs(t, cB, p, nil); st != 404 {
			t.Errorf("%s: %d", p, st)
		}
	}
	var fp stats.FailurePage
	if x.getAs(t, cB, "/api/v1/stats/failures?"+rng, &fp); len(fp.Items) != 0 || len(fp.ByCategory) != 0 {
		t.Fatalf("failures leak: %+v", fp)
	}
	for _, q := range []string{"zebra", "handl", "beta", "bash"} {
		var r stats.SearchResult
		if x.getAs(t, cB, "/api/v1/stats/search?q="+q, &r); len(r.Hits) != 0 {
			t.Errorf("search %q leaks: %+v", q, r.Hits)
		}
	}
	req, _ := http.NewRequest("GET", x.URL+"/v1/stats/self?session=s1", nil)
	req.Header.Set("Authorization", "Bearer "+tokB)
	req.Header.Set("Braid-Protocol", "1")
	resp, err := x.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"session":null`) || !strings.Contains(string(body), `"sessions":0`) {
		t.Fatalf("self leaks: %d %s", resp.StatusCode, body)
	}
	for _, table := range []string{"projects", "sessions", "session_devices", "agents", "tool_calls", "skill_phases", "usage", "failures", "search_docs", "derive_cursor", "derive_errors"} {
		err := x.dt.DB.WithUser(context.Background(), meB.ID, func(tx pgx.Tx) error {
			var n int
			if err := tx.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
				return err
			}
			if n != 0 {
				t.Errorf("table %s shows %d rows to user B", table, n)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
