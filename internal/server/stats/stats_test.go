// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

//nolint:bodyclose,errcheck,gosec,noctx // test helpers
package stats_test

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"testing"

	"github.com/usebraid/braid/internal/server/api"
	dt "github.com/usebraid/braid/internal/server/derive/derivetest"
	"github.com/usebraid/braid/internal/server/servertest"
)

type env struct {
	*servertest.Env
	dt       *dt.Env
	c        *http.Client
	uid, dev string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := servertest.Start(t)
	x := &env{Env: e, dt: dt.Wrap(t, e.OwnerURL, e.AppURL), c: e.Login(t, "u1")}
	var me api.Me
	if st := x.getAs(t, x.c, "/api/v1/me", &me); st != 200 {
		t.Fatalf("me: %d", st)
	}
	x.uid = me.ID
	_, x.dev = e.NewDevice(t, x.c, "laptop")
	return x
}

func (x *env) getAs(t *testing.T, c *http.Client, path string, out any) int {
	t.Helper()
	resp, err := c.Get(x.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if out != nil && resp.StatusCode == 200 {
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatalf("%v: %s", err, b)
		}
	}
	return resp.StatusCode
}

func (x *env) get(t *testing.T, path string, out any) int {
	t.Helper()
	return x.getAs(t, x.c, path, out)
}

func (x *env) setTZ(t *testing.T, tz string) {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"time_zone": tz})
	req, _ := http.NewRequest("PATCH", x.URL+"/api/v1/me", bytes.NewReader(b))
	req.Header.Set("Origin", x.URL)
	req.Header.Set("Content-Type", "application/json")
	resp, err := x.c.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("set tz: %v %v", resp, err)
	}
	resp.Body.Close()
}

const rng = "from=2026-10-01T00:00:00Z&to=2026-12-01T00:00:00Z"
const opus, sonnet = "claude-opus-5-5", "claude-sonnet-5-5"

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
