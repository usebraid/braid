// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

//nolint:bodyclose // do() closes the body
package api_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/usebraid/braid/internal/server/api"
	dt "github.com/usebraid/braid/internal/server/derive/derivetest"
	"github.com/usebraid/braid/internal/server/servertest"
)

func TestAdminStats(t *testing.T) {
	e := servertest.Start(t)
	admin := e.Login(t, "root", "braid-admins")
	alice := e.Login(t, "alice")
	_, b := do(t, e, alice, "GET", "/api/v1/me", nil)
	var me api.Me
	_ = json.Unmarshal(b, &me)
	x := dt.Wrap(t, e.OwnerURL, e.AppURL)
	dev := x.Device(t, me.ID, "laptop")
	x.Put(t, me.ID, dev, dt.File{Rel: "-w/s1.jsonl", Lines: []string{
		dt.Prompt(dt.At(0), "Secret prompt text"),
		dt.Reply(dt.At(5), "m1", "claude-opus-5-5", dt.Usage(1000, 100, 0, 0, 0), dt.Text("secret answer"))}})
	x.Derive(t, me.ID, 0)

	resp, body := do(t, e, admin, "GET", "/api/v1/admin/stats", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	var st api.AdminStats
	if err := json.Unmarshal(body, &st); err != nil {
		t.Fatal(err)
	}
	byEmail := map[string]api.AdminUserStats{}
	for _, u := range st.Users {
		byEmail[u.Email] = u
	}
	a := byEmail["alice@example.test"]
	if len(st.Users) != 2 || a.Sessions != 1 || a.Tokens != 1100 || a.CostUSD < 0.0059 || a.CostUSD > 0.0061 || a.LastActivityAt == nil || a.Rebuilding || a.Devices != 1 {
		t.Fatalf("%+v", st)
	}
	if byEmail["root@example.test"].Sessions != 0 || st.Totals.Users != 2 || st.Totals.Sessions != 1 || st.Totals.Tokens != 1100 {
		t.Fatalf("%+v", st)
	}
	if strings.Contains(string(body), "Secret") || strings.Contains(string(body), "secret") {
		t.Fatalf("content in admin stats: %s", body)
	}
	if resp, _ := do(t, e, alice, "GET", "/api/v1/admin/stats", nil); resp.StatusCode != 403 {
		t.Fatalf("non-admin: %d", resp.StatusCode)
	}
}
