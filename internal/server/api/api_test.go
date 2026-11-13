// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

//nolint:bodyclose,errcheck,gosec,noctx // test helpers: short-lived requests, body closed inside do
package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/usebraid/braid/internal/server"
	"github.com/usebraid/braid/internal/server/api"
	"github.com/usebraid/braid/internal/server/servertest"
	"github.com/usebraid/braid/internal/server/webauth/oidctest"
	"github.com/usebraid/braid/internal/testdb"
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
	for _, bad := range []string{"a\u202eb", "a\u200bb", "a\x00b", "\u00a0"} {
		if resp, _ := do(t, e, c, "PATCH", "/api/v1/devices/"+id, map[string]string{"name": bad}); resp.StatusCode != 400 {
			t.Fatalf("name %q: %d", bad, resp.StatusCode)
		}
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

func TestDeleteUserWithoutEmailNeedsConfirm(t *testing.T) {
	e := servertest.Start(t)
	admin := e.Login(t, "root", "braid-admins")
	_ = e.Login(t, "noemail")
	testdb.Exec(t, e.OwnerURL, `UPDATE users SET email = '' WHERE oidc_sub = 'noemail'`)
	_, b := do(t, e, admin, "GET", "/api/v1/admin/users", nil)
	var us []api.AdminUser
	_ = json.Unmarshal(b, &us)
	var uid string
	for _, u := range us {
		if u.Name == "noemail" {
			uid = u.ID
		}
	}
	if uid == "" {
		t.Fatalf("%s", b)
	}
	if resp, _ := do(t, e, admin, "DELETE", "/api/v1/admin/users/"+uid, nil); resp.StatusCode != 400 {
		t.Fatalf("unconfirmed delete: %d", resp.StatusCode)
	}
	if resp, _ := do(t, e, admin, "DELETE", "/api/v1/admin/users/"+uid+"?confirm="+uid, nil); resp.StatusCode != 204 {
		t.Fatalf("confirmed delete: %d", resp.StatusCode)
	}
}

func TestMeTimeZone(t *testing.T) {
	e := servertest.Start(t)
	c := e.Login(t, "u1")
	get := func(c *http.Client) api.Me {
		_, b := do(t, e, c, "GET", "/api/v1/me", nil)
		var m api.Me
		_ = json.Unmarshal(b, &m)
		return m
	}
	if m := get(c); m.TimeZone != "UTC" || m.TimeZoneSet {
		t.Fatalf("%+v", m)
	}
	if resp, b := do(t, e, c, "PATCH", "/api/v1/me", map[string]string{"time_zone": "Europe/Berlin"}); resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	if m := get(c); m.TimeZone != "Europe/Berlin" || !m.TimeZoneSet {
		t.Fatalf("%+v", m)
	}
	for name, body := range map[string]map[string]string{
		"unknown zone": {"time_zone": "Mars/Base"}, "empty": {"time_zone": ""}, "missing": {},
		"extra field": {"time_zone": "UTC", "is_admin": "true"}, "too long": {"time_zone": strings.Repeat("a", 65)},
	} {
		if resp, _ := do(t, e, c, "PATCH", "/api/v1/me", body); resp.StatusCode != 400 {
			t.Errorf("%s: status %d", name, resp.StatusCode)
		}
	}
	if m := get(e.Login(t, "u2")); m.TimeZone != "UTC" || m.TimeZoneSet {
		t.Fatalf("another user's zone changed: %+v", m)
	}
}

func TestAvatarIsTheUsersOwnPicture(t *testing.T) {
	const pic = "https://idp.example/avatars/alice.png?sig=abc"
	img := []byte("\x89PNG\r\n\x1a\nalice")
	var fetched []string
	e := servertest.Start(t, server.WithAvatarFetch(func(_ context.Context, raw string) ([]byte, string, error) {
		fetched = append(fetched, raw)
		if raw == pic {
			return img, "image/png", nil
		}
		return nil, "", errors.New("refused")
	}))
	alice := e.LoginUser(t, oidctest.User{Sub: "alice", Email: "alice@x.test", Name: "alice", Picture: pic})
	resp, b := do(t, e, alice, "GET", "/api/v1/me/avatar", nil)
	if resp.StatusCode != 200 || !bytes.Equal(b, img) || resp.Header.Get("Content-Type") != "image/png" ||
		resp.Header.Get("Cache-Control") != "private, max-age=3600" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("alice: %d %q %v", resp.StatusCode, b, resp.Header)
	}
	// bob has no picture: his request after alice's (cached) one must not see hers
	bob := e.Login(t, "bob")
	if resp, b := do(t, e, bob, "GET", "/api/v1/me/avatar", nil); resp.StatusCode != 404 || bytes.Contains(b, img) {
		t.Fatalf("bob: %d %q", resp.StatusCode, b)
	}
	// bob with a picture the fetcher refuses
	e.LoginUser(t, oidctest.User{Sub: "bob", Email: "bob@x.test", Name: "bob", Picture: "https://evil.example/x.png"})
	if resp, _ := do(t, e, bob, "GET", "/api/v1/me/avatar", nil); resp.StatusCode != 404 {
		t.Fatalf("refused picture: %d", resp.StatusCode)
	}
	if resp, _ := do(t, e, e.Client(), "GET", "/api/v1/me/avatar", nil); resp.StatusCode != 401 {
		t.Fatalf("anonymous: %d", resp.StatusCode)
	}
	if resp, _ := do(t, e, alice, "GET", "/api/v1/me/avatar", nil); resp.StatusCode != 200 || len(fetched) != 2 {
		t.Fatalf("alice again: %d, fetches %v (the second read is cached)", resp.StatusCode, fetched)
	}
	// a later login without the claim clears the stored picture
	e.LoginUser(t, oidctest.User{Sub: "alice", Email: "alice@x.test", Name: "alice"})
	if resp, _ := do(t, e, alice, "GET", "/api/v1/me/avatar", nil); resp.StatusCode != 404 {
		t.Fatalf("cleared picture: %d", resp.StatusCode)
	}
}
