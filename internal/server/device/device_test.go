// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

//nolint:errcheck,gosec,noctx,revive // brief-mandated test code; response bodies in short-lived tests, no ctx needed
package device_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/usebraid/braid/internal/protocol"
	"github.com/usebraid/braid/internal/server/servertest"
	"github.com/usebraid/braid/internal/testdb"
)

func codeReq(t *testing.T, e *servertest.Env) protocol.DeviceCodeResponse {
	t.Helper()
	body, _ := json.Marshal(protocol.DeviceCodeRequest{Hostname: "mac-<b>", OS: "darwin", Arch: "arm64", OSUser: "jan", DaemonVersion: "0.1.0"})
	resp, err := e.Client().Post(e.URL+"/v1/device/code", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("code: %d", resp.StatusCode)
	}
	var out protocol.DeviceCodeResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}

func poll(t *testing.T, e *servertest.Env, dc string) (int, string, protocol.TokenResponse) {
	t.Helper()
	body, _ := json.Marshal(protocol.TokenRequest{GrantType: protocol.GrantTypeDeviceCode, DeviceCode: dc})
	resp, err := e.Client().Post(e.URL+"/v1/device/token", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var oe protocol.OAuthError
	var tr protocol.TokenResponse
	_ = json.Unmarshal(raw, &oe)
	_ = json.Unmarshal(raw, &tr)
	return resp.StatusCode, oe.Error, tr
}

func form(t *testing.T, e *servertest.Env, c *http.Client, path string, v url.Values) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", e.URL+path, strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", e.URL)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestCodeResponseShape(t *testing.T) {
	e := servertest.Start(t)
	r := codeReq(t, e)
	if _, err := protocol.NormalizeUserCode(r.UserCode); err != nil {
		t.Fatalf("user code %q", r.UserCode)
	}
	if r.VerificationURI != e.URL+"/device" || r.VerificationURIComplete != e.URL+"/device?user_code="+r.UserCode {
		t.Fatalf("%+v", r)
	}
	if r.ExpiresIn != 600 || r.Interval != 5 || len(r.DeviceCode) < 40 {
		t.Fatalf("%+v", r)
	}
}

func TestFullFlow(t *testing.T) {
	e := servertest.Start(t)
	c := e.Login(t, "u1")
	r := codeReq(t, e)
	if code, status, _ := poll(t, e, r.DeviceCode); code != 400 || status != "authorization_pending" {
		t.Fatalf("%d %s", code, status)
	}
	page, _ := c.Get(r.VerificationURIComplete)
	html, _ := io.ReadAll(page.Body)
	page.Body.Close()
	for _, want := range []string{"mac-&lt;b&gt;", "darwin", "arm64", "jan", "0.1.0"} {
		if !strings.Contains(string(html), want) {
			t.Errorf("confirmation page lacks %q", want)
		}
	}
	if strings.Contains(string(html), "mac-<b>") {
		t.Error("hostname not escaped")
	}
	form(t, e, c, "/device/approve", url.Values{"user_code": {r.UserCode}, "name": {"Laptop"}}).Body.Close()
	testdb.Exec(t, e.OwnerURL, `UPDATE device_codes SET last_poll_at = now() - interval '1 hour'`)
	code, _, tr := poll(t, e, r.DeviceCode)
	if code != 200 || !strings.HasPrefix(tr.AccessToken, protocol.TokenPrefix) || tr.TokenType != "Bearer" || tr.Scope != "ingest:write stats:read:self" || tr.DeviceID == "" {
		t.Fatalf("%d %+v", code, tr)
	}
	testdb.Exec(t, e.OwnerURL, `UPDATE device_codes SET last_poll_at = now() - interval '1 hour'`)
	if code, status, _ := poll(t, e, r.DeviceCode); code != 400 || status != "invalid_grant" {
		t.Fatalf("reuse: %d %s", code, status)
	}
}

func TestSlowDown(t *testing.T) {
	e := servertest.Start(t)
	r := codeReq(t, e)
	poll(t, e, r.DeviceCode)
	if _, status, _ := poll(t, e, r.DeviceCode); status != "slow_down" {
		t.Fatalf("got %s", status)
	}
}

func TestDeny(t *testing.T) {
	e := servertest.Start(t)
	c := e.Login(t, "u1")
	r := codeReq(t, e)
	form(t, e, c, "/device/deny", url.Values{"user_code": {r.UserCode}}).Body.Close()
	testdb.Exec(t, e.OwnerURL, `UPDATE device_codes SET last_poll_at = now() - interval '1 hour'`)
	if _, status, _ := poll(t, e, r.DeviceCode); status != "access_denied" {
		t.Fatalf("got %s", status)
	}
}

func TestDevicePageRequiresLogin(t *testing.T) {
	e := servertest.Start(t)
	c := e.Client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, _ := c.Get(e.URL + "/device?user_code=BCDF-GHJK")
	resp.Body.Close()
	if resp.StatusCode != 302 || !strings.HasPrefix(resp.Header.Get("Location"), "/auth/login?next=") {
		t.Fatalf("%d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestApproveRequiresOrigin(t *testing.T) {
	e := servertest.Start(t)
	c := e.Login(t, "u1")
	r := codeReq(t, e)
	req, _ := http.NewRequest("POST", e.URL+"/device/approve", strings.NewReader(url.Values{"user_code": {r.UserCode}, "name": {"x"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.example")
	resp, _ := c.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("cross-origin approve: %d", resp.StatusCode)
	}
}

func TestUserCodeBruteForceLimited(t *testing.T) {
	e := servertest.Start(t)
	c := e.Login(t, "u1")
	last := 0
	for i := 0; i < 6; i++ {
		resp, _ := c.Get(e.URL + "/device?user_code=BBBB-BBB" + string("BCDFGH"[i]))
		last = resp.StatusCode
		resp.Body.Close()
	}
	if last != 429 {
		t.Fatalf("6th wrong code: %d", last)
	}
}

func TestCodeEndpointRateLimited(t *testing.T) {
	e := servertest.Start(t)
	body, _ := json.Marshal(protocol.DeviceCodeRequest{Hostname: "h", OS: "linux", Arch: "amd64", OSUser: "u", DaemonVersion: "0.1.0"})
	last := 0
	for i := 0; i < 11; i++ {
		resp, _ := e.Client().Post(e.URL+"/v1/device/code", "application/json", bytes.NewReader(body))
		last = resp.StatusCode
		resp.Body.Close()
	}
	if last != 429 {
		t.Fatalf("11th request: %d", last)
	}
}

func TestRevokeViaDaemon(t *testing.T) {
	e := servertest.Start(t)
	c := e.Login(t, "u1")
	tok, _ := e.NewDevice(t, c, "Laptop")
	req, _ := http.NewRequest("POST", e.URL+"/v1/device/revoke", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set(protocol.HeaderVersion, "1")
	resp, _ := e.Client().Do(req)
	resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Fatalf("revoke: %d", resp.StatusCode)
	}
	req2, _ := http.NewRequest("GET", e.URL+"/v1/files", nil)
	req2.Header.Set("Authorization", "Bearer "+tok)
	req2.Header.Set(protocol.HeaderVersion, "1")
	r2, _ := e.Client().Do(req2)
	r2.Body.Close()
	if r2.StatusCode != 401 {
		t.Fatalf("after revoke: %d", r2.StatusCode)
	}
}

func TestHealthz(t *testing.T) {
	e := servertest.Start(t)
	resp, _ := e.Client().Get(e.URL + "/healthz")
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(b) != "ok" {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
}

func TestBruteForceLockoutBlocksCorrectCode(t *testing.T) {
	e := servertest.Start(t)
	c := e.Login(t, "u1")
	r := codeReq(t, e)
	for i := 0; i < 5; i++ {
		resp, _ := c.Get(e.URL + "/device?user_code=BBBB-BBB" + string("BCDFG"[i]))
		resp.Body.Close()
	}
	resp, _ := c.Get(r.VerificationURIComplete)
	resp.Body.Close()
	if resp.StatusCode != 429 {
		t.Fatalf("correct code after lockout: %d", resp.StatusCode)
	}
	resp = form(t, e, c, "/device/approve", url.Values{"user_code": {r.UserCode}, "name": {"x"}})
	resp.Body.Close()
	if resp.StatusCode != 429 {
		t.Fatalf("approve after lockout: %d", resp.StatusCode)
	}
}
