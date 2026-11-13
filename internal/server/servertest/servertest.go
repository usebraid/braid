// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package servertest starts a complete braid-server against a fresh test database and a mock OIDC provider.
//
//nolint:errcheck,gosec,noctx,revive // test support: short-lived requests; IdP name fixed by the brief
package servertest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/usebraid/braid/internal/protocol"
	"github.com/usebraid/braid/internal/server"
	"github.com/usebraid/braid/internal/server/config"
	"github.com/usebraid/braid/internal/server/webauth"
	"github.com/usebraid/braid/internal/server/webauth/oidctest"
	"github.com/usebraid/braid/internal/testdb"
)

// Env is a running test server.
type Env struct {
	URL      string
	OwnerURL string
	AppURL   string
	IdP      *oidctest.Provider
	Client   func() *http.Client // fresh TLS client with cookie jar
}

// Start builds the server; everything is torn down when t finishes.
func Start(t testing.TB, opts ...server.Option) *Env {
	t.Helper()
	ownerURL, appURL := testdb.New(t)
	idp := oidctest.New(t, "braid", "secret")
	var h atomic.Value // PublicURL is only known once the TLS server is up
	h.Store(http.NotFoundHandler())
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.Load().(http.Handler).ServeHTTP(w, r)
	}))
	srv.StartTLS()
	t.Cleanup(srv.Close)
	pub, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	s, err := server.New(context.Background(), config.Config{PublicURL: pub, DatabaseURL: appURL, DatabaseOwnerURL: ownerURL,
		OIDCIssuer: idp.Issuer, OIDCClientID: "braid", OIDCClientSecret: "secret",
		OIDCScopes: []string{"openid", "profile", "email", "groups"}, OIDCGroupsClaim: "groups", OIDCAdminGroup: "braid-admins"}, opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	h.Store(s.Handler())
	return &Env{URL: srv.URL, OwnerURL: ownerURL, AppURL: appURL, IdP: idp, Client: func() *http.Client {
		jar, _ := cookiejar.New(nil)
		return &http.Client{Transport: srv.Client().Transport, Jar: jar}
	}}
}

// Login returns a client logged in as sub with groups.
func (e *Env) Login(t testing.TB, sub string, groups ...string) *http.Client {
	t.Helper()
	e.IdP.SetUser(oidctest.User{Sub: sub, Email: sub + "@example.test", Name: sub, Groups: groups})
	c := e.Client()
	resp, err := c.Get(e.URL + "/auth/login")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	u, _ := url.Parse(e.URL)
	for _, ck := range c.Jar.Cookies(u) {
		if ck.Name == webauth.CookieName {
			return c
		}
	}
	t.Fatalf("login as %s failed: status %d", sub, resp.StatusCode)
	return nil
}

// NewDevice runs the complete device flow: code, approve via c, poll; returns token and device id.
func (e *Env) NewDevice(t testing.TB, c *http.Client, name string) (token, deviceID string) {
	t.Helper()
	body, _ := json.Marshal(protocol.DeviceCodeRequest{Hostname: "test-host", OS: "linux", Arch: "amd64", OSUser: "tester", DaemonVersion: "0.1.0"})
	resp, err := e.Client().Post(e.URL+"/v1/device/code", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var dc protocol.DeviceCodeResponse
	err = json.NewDecoder(resp.Body).Decode(&dc)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("device code: %d %v", resp.StatusCode, err)
	}
	req, _ := http.NewRequest("POST", e.URL+"/device/approve", strings.NewReader(url.Values{"user_code": {dc.UserCode}, "name": {name}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", e.URL)
	ar, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	ar.Body.Close()
	if ar.StatusCode != 200 {
		t.Fatalf("approve: %d", ar.StatusCode)
	}
	testdb.Exec(t, e.OwnerURL, `UPDATE device_codes SET last_poll_at = NULL`) // avoid slow_down
	pb, _ := json.Marshal(protocol.TokenRequest{GrantType: protocol.GrantTypeDeviceCode, DeviceCode: dc.DeviceCode})
	pr, err := e.Client().Post(e.URL+"/v1/device/token", "application/json", bytes.NewReader(pb))
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Body.Close()
	var tr protocol.TokenResponse
	if err := json.NewDecoder(pr.Body).Decode(&tr); err != nil || pr.StatusCode != 200 {
		t.Fatalf("poll: %d %v", pr.StatusCode, err)
	}
	return tr.AccessToken, tr.DeviceID
}
