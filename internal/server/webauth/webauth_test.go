// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

//nolint:errcheck,gosec,noctx,revive // brief-mandated test code; response bodies in short-lived tests, no ctx needed
package webauth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/usebraid/braid/internal/server/config"
	"github.com/usebraid/braid/internal/server/db"
	"github.com/usebraid/braid/internal/server/httpx"
	"github.com/usebraid/braid/internal/server/webauth"
	"github.com/usebraid/braid/internal/server/webauth/oidctest"
	"github.com/usebraid/braid/internal/testdb"
)

type env struct {
	srv   *httptest.Server
	idp   *oidctest.Provider
	owner string
}

func start(t *testing.T, mutate func(*config.Config)) env {
	t.Helper()
	ctx := context.Background()
	ownerURL, appURL := testdb.New(t)
	d, err := db.Open(ctx, appURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	idp := oidctest.New(t, "braid", "secret")
	mux := http.NewServeMux()
	srv := httptest.NewUnstartedServer(nil)
	srv.StartTLS()
	t.Cleanup(srv.Close)
	pub, _ := url.Parse(srv.URL)
	cfg := config.Config{PublicURL: pub, OIDCIssuer: idp.Issuer, OIDCClientID: "braid", OIDCClientSecret: "secret",
		OIDCScopes: []string{"openid", "profile", "email", "groups"}, OIDCGroupsClaim: "groups", OIDCAdminGroup: "braid-admins"}
	if mutate != nil {
		mutate(&cfg)
	}
	a, err := webauth.New(ctx, cfg, d, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	a.Register(mux)
	mux.Handle("GET /api/v1/whoami", a.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := webauth.PrincipalFrom(r.Context())
		httpx.WriteJSON(w, 200, p)
	})))
	mux.Handle("GET /api/v1/adminonly", a.Require(a.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))))
	srv.Config.Handler = httpx.SecurityHeaders(mux)
	return env{srv: srv, idp: idp, owner: ownerURL}
}

func (e env) client(t *testing.T) *http.Client {
	jar, _ := cookiejar.New(nil)
	c := e.srv.Client()
	c.Jar = jar
	return c
}

func login(t *testing.T, e env, c *http.Client, u oidctest.User) *http.Response {
	t.Helper()
	e.idp.SetUser(u)
	resp, err := c.Get(e.srv.URL + "/auth/login?next=/api/v1/whoami")
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestLoginHappyPath(t *testing.T) {
	e := start(t, nil)
	c := e.client(t)
	resp := login(t, e, c, oidctest.User{Sub: "u1", Email: "u1@x", Name: "U1", Groups: []string{"braid-admins"}})
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var p webauth.Principal
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	if p.UserID == "" || !p.IsAdmin {
		t.Fatalf("%+v", p)
	}
	u, _ := url.Parse(e.srv.URL)
	for _, ck := range c.Jar.Cookies(u) {
		if ck.Name == webauth.CookieName {
			return
		}
	}
	t.Fatal("session cookie missing")
}

func TestUnauthenticatedAPIGets401(t *testing.T) {
	e := start(t, nil)
	resp, err := e.client(t).Get(e.srv.URL + "/api/v1/whoami")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestMissingGroupsClaimMeansNoAdmin(t *testing.T) {
	e := start(t, nil)
	c := e.client(t)
	resp := login(t, e, c, oidctest.User{Sub: "u2", Email: "u2@x", Name: "U2"})
	resp.Body.Close()
	r2, _ := c.Get(e.srv.URL + "/api/v1/adminonly")
	r2.Body.Close()
	if r2.StatusCode != 403 {
		t.Fatalf("non-admin got %d", r2.StatusCode)
	}
}

func TestAllowlist(t *testing.T) {
	e := start(t, func(c *config.Config) { c.OIDCAllowedGroups = []string{"braid-users"} })
	resp := login(t, e, e.client(t), oidctest.User{Sub: "out", Groups: []string{"other"}})
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("outsider got %d", resp.StatusCode)
	}
	resp = login(t, e, e.client(t), oidctest.User{Sub: "in", Groups: []string{"braid-users"}})
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("member got %d", resp.StatusCode)
	}
	resp = login(t, e, e.client(t), oidctest.User{Sub: "adm", Groups: []string{"braid-admins"}})
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("admin outside allowlist got %d", resp.StatusCode)
	}
}

func TestTamperedTokensRejected(t *testing.T) {
	for name, tamper := range map[string]func(*oidctest.Provider){
		"nonce":    func(p *oidctest.Provider) { p.WrongNonce = true },
		"audience": func(p *oidctest.Provider) { p.WrongAudience = true },
		"issuer":   func(p *oidctest.Provider) { p.WrongIssuer = true },
		"expired":  func(p *oidctest.Provider) { p.ExpiredToken = true },
	} {
		t.Run(name, func(t *testing.T) {
			e := start(t, nil)
			tamper(e.idp)
			resp := login(t, e, e.client(t), oidctest.User{Sub: "u"})
			resp.Body.Close()
			if resp.StatusCode == 200 {
				t.Fatal("tampered login succeeded")
			}
		})
	}
}

func TestCallbackWithoutStateCookieRejected(t *testing.T) {
	e := start(t, nil)
	resp, err := e.client(t).Get(e.srv.URL + "/auth/callback?code=x&state=y")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestOpenRedirectBlocked(t *testing.T) {
	e := start(t, nil)
	c := e.client(t)
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Host == "evil.example" {
			t.Fatal("redirected to evil.example")
		}
		return nil
	}
	e.idp.SetUser(oidctest.User{Sub: "u"})
	resp, err := c.Get(e.srv.URL + "/auth/login?next=//evil.example/x")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func TestLogoutInvalidatesSession(t *testing.T) {
	e := start(t, nil)
	c := e.client(t)
	login(t, e, c, oidctest.User{Sub: "u"}).Body.Close()
	req, _ := http.NewRequest("POST", e.srv.URL+"/auth/logout", nil)
	req.Header.Set("Origin", e.srv.URL)
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	c.CheckRedirect = nil
	r2, _ := c.Get(e.srv.URL + "/api/v1/whoami")
	r2.Body.Close()
	if r2.StatusCode != 401 {
		t.Fatalf("after logout: %d", r2.StatusCode)
	}
}

func TestDisabledUserCannotLogin(t *testing.T) {
	e := start(t, nil)
	login(t, e, e.client(t), oidctest.User{Sub: "u"}).Body.Close()
	testdb.Exec(t, e.owner, `UPDATE users SET disabled_at = now() WHERE oidc_sub = 'u'`)
	resp := login(t, e, e.client(t), oidctest.User{Sub: "u"})
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("disabled user got %d", resp.StatusCode)
	}
}

func TestSessionCookieRefreshedAndLoginAudited(t *testing.T) {
	e := start(t, nil)
	c := e.client(t)
	login(t, e, c, oidctest.User{Sub: "aud"}).Body.Close()
	resp, err := c.Get(e.srv.URL + "/api/v1/whoami")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	ok := false
	for _, ck := range resp.Cookies() {
		ok = ok || (ck.Name == webauth.CookieName && ck.MaxAge == 604800)
	}
	if !ok {
		t.Fatal("no refreshed session cookie with Max-Age=604800")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, e.owner)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var n int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM audit_log a JOIN users u ON u.id = a.subject_user_id
		WHERE a.action = 'login' AND u.oidc_sub = 'aud'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("login audit rows = %d, err %v", n, err)
	}
}
