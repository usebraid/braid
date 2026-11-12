// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

//nolint:bodyclose,errcheck,gosec,noctx // test helpers: short-lived requests, body closed inside do
package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/usebraid/braid/internal/server/api"
	"github.com/usebraid/braid/internal/server/servertest"
)

func do(t *testing.T, e *servertest.Env, c *http.Client, method, path string, body any) (*http.Response, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, e.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", e.URL)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func filesStatus(t *testing.T, e *servertest.Env, token string) int {
	t.Helper()
	req, _ := http.NewRequest("GET", e.URL+"/v1/files", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Braid-Protocol", "1")
	resp, err := e.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestMe(t *testing.T) {
	e := servertest.Start(t)
	c := e.Login(t, "u1", "braid-admins")
	_, b := do(t, e, c, "GET", "/api/v1/me", nil)
	var me api.Me
	_ = json.Unmarshal(b, &me)
	if me.ID == "" || !me.IsAdmin {
		t.Fatalf("%s", b)
	}
}

func TestDevicesListRenameRevoke(t *testing.T) {
	e := servertest.Start(t)
	c := e.Login(t, "u1")
	tok, id := e.NewDevice(t, c, "Laptop")
	_, b := do(t, e, c, "GET", "/api/v1/devices", nil)
	var ds []api.Device
	_ = json.Unmarshal(b, &ds)
	if len(ds) != 1 || ds[0].Name != "Laptop" || ds[0].OSUser == "" {
		t.Fatalf("%s", b)
	}
	resp, b := do(t, e, c, "PATCH", "/api/v1/devices/"+id, map[string]string{"name": "Work Mac"})
	if resp.StatusCode != 200 || !strings.Contains(string(b), "Work Mac") {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	if resp, _ := do(t, e, c, "PATCH", "/api/v1/devices/"+id, map[string]string{"name": ""}); resp.StatusCode != 400 {
		t.Fatalf("empty name: %d", resp.StatusCode)
	}
	if resp, _ := do(t, e, c, "DELETE", "/api/v1/devices/"+id, nil); resp.StatusCode != 204 {
		t.Fatalf("revoke: %d", resp.StatusCode)
	}
	if code := filesStatus(t, e, tok); code != 401 {
		t.Fatalf("token after revoke: %d", code)
	}
	_, b = do(t, e, c, "GET", "/api/v1/audit", nil)
	for _, a := range []string{"device_approved", "device_renamed", "device_revoked", "login"} {
		if !strings.Contains(string(b), a) {
			t.Errorf("audit lacks %s", a)
		}
	}
}

func TestForeignDeviceIs404(t *testing.T) {
	e := servertest.Start(t)
	a := e.Login(t, "a")
	b := e.Login(t, "b")
	_, id := e.NewDevice(t, a, "A's")
	for _, m := range []string{"PATCH", "DELETE"} {
		resp, _ := do(t, e, b, m, "/api/v1/devices/"+id, map[string]string{"name": "x"})
		if resp.StatusCode != 404 {
			t.Errorf("%s foreign device: %d", m, resp.StatusCode)
		}
	}
	if resp, _ := do(t, e, b, "DELETE", "/api/v1/devices/not-a-uuid", nil); resp.StatusCode != 404 {
		t.Errorf("bad id: %d", resp.StatusCode)
	}
	_, body := do(t, e, b, "GET", "/api/v1/audit", nil)
	if strings.Contains(string(body), "device_approved") {
		t.Error("B sees A's audit entries")
	}
}

func TestAdminEndpoints(t *testing.T) {
	e := servertest.Start(t)
	admin := e.Login(t, "root", "braid-admins")
	user := e.Login(t, "u1")
	if resp, _ := do(t, e, user, "GET", "/api/v1/admin/users", nil); resp.StatusCode != 403 {
		t.Fatalf("non-admin: %d", resp.StatusCode)
	}
	_, b := do(t, e, admin, "GET", "/api/v1/admin/users", nil)
	var us []api.AdminUser
	_ = json.Unmarshal(b, &us)
	var uid, email string
	for _, u := range us {
		if u.Name == "u1" {
			uid, email = u.ID, u.Email
		}
	}
	if uid == "" {
		t.Fatalf("%s", b)
	}
	if resp, _ := do(t, e, admin, "POST", "/api/v1/admin/users/"+uid+"/disable", nil); resp.StatusCode != 204 {
		t.Fatal(resp.StatusCode)
	}
	if resp, _ := do(t, e, user, "GET", "/api/v1/me", nil); resp.StatusCode != 401 {
		t.Fatalf("disabled user session: %d", resp.StatusCode)
	}
	if resp, _ := do(t, e, admin, "DELETE", "/api/v1/admin/users/"+uid+"?confirm=wrong", nil); resp.StatusCode != 400 {
		t.Fatal(resp.StatusCode)
	}
	if resp, _ := do(t, e, admin, "DELETE", "/api/v1/admin/users/"+uid+"?confirm="+url.QueryEscape(email), nil); resp.StatusCode != 204 {
		t.Fatal(resp.StatusCode)
	}
	_, b = do(t, e, admin, "GET", "/api/v1/admin/audit", nil)
	for _, a := range []string{"user_disabled", "user_deleted"} {
		if !strings.Contains(string(b), a) {
			t.Errorf("admin audit lacks %s", a)
		}
	}
}

func TestCrossOriginMutationRejected(t *testing.T) {
	e := servertest.Start(t)
	c := e.Login(t, "u1")
	_, id := e.NewDevice(t, c, "L")
	req, _ := http.NewRequest("DELETE", e.URL+"/api/v1/devices/"+id, nil)
	req.Header.Set("Origin", "https://evil.example")
	resp, _ := c.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("%d", resp.StatusCode)
	}
}

func TestAdminSelfAndBadIDs(t *testing.T) {
	e := servertest.Start(t)
	admin := e.Login(t, "root", "braid-admins")
	_, b := do(t, e, admin, "GET", "/api/v1/me", nil)
	var me api.Me
	_ = json.Unmarshal(b, &me)
	for _, c := range [][2]string{{"POST", "/disable"}, {"POST", "/enable"}, {"DELETE", "?confirm=" + url.QueryEscape(me.Email)}} {
		if resp, _ := do(t, e, admin, c[0], "/api/v1/admin/users/"+me.ID+c[1], nil); resp.StatusCode != 400 {
			t.Errorf("self %s %s: %d", c[0], c[1], resp.StatusCode)
		}
		if resp, _ := do(t, e, admin, c[0], "/api/v1/admin/users/not-a-uuid"+c[1], nil); resp.StatusCode != 404 {
			t.Errorf("bad id %s %s: %d", c[0], c[1], resp.StatusCode)
		}
	}
}
