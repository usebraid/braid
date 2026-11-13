// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package webauth implements OIDC web login (authorization code + PKCE) and DB-backed sessions.
package webauth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"github.com/usebraid/braid/internal/server/config"
	"github.com/usebraid/braid/internal/server/db"
	"github.com/usebraid/braid/internal/server/httpx"
)

const loginCookie = "__Host-braid_login"

// Auth serves the /auth/* routes and the session middleware.
type Auth struct {
	cfg        config.Config
	oauth      oauth2.Config
	verifier   *oidc.IDTokenVerifier
	endSession string
	db         *db.DB
	now        func() time.Time
	limiter    *httpx.Limiter
	key        []byte // signs the login-state cookie; per process
}

// New runs OIDC discovery.
func New(ctx context.Context, cfg config.Config, d *db.DB, now func() time.Time) (*Auth, error) {
	prov, err := oidc.NewProvider(ctx, cfg.OIDCIssuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	var meta struct {
		EndSession string `json:"end_session_endpoint"`
	}
	if err := prov.Claims(&meta); err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return &Auth{
		cfg: cfg, db: d, now: now, key: key, endSession: meta.EndSession,
		oauth: oauth2.Config{ClientID: cfg.OIDCClientID, ClientSecret: cfg.OIDCClientSecret, Endpoint: prov.Endpoint(),
			RedirectURL: cfg.PublicURL.String() + "/auth/callback", Scopes: cfg.OIDCScopes},
		verifier: prov.Verifier(&oidc.Config{ClientID: cfg.OIDCClientID, Now: now}),
		limiter:  httpx.NewLimiter(30, now),
	}, nil
}

// Register mounts /auth/login, /auth/callback and /auth/logout.
func (a *Auth) Register(mux *http.ServeMux) {
	mux.Handle("GET /auth/login", a.limit(http.HandlerFunc(a.login)))
	mux.Handle("GET /auth/callback", a.limit(http.HandlerFunc(a.callback)))
	mux.Handle("POST /auth/logout", a.limit(httpx.RequireSameOrigin(a.cfg.PublicURL, http.HandlerFunc(a.logout))))
}

func (a *Auth) limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.limiter.Allow(httpx.ClientIP(r, a.cfg.TrustedProxies).String()) {
			httpx.TooMany(w, time.Minute)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type loginState struct {
	State, Nonce, Verifier, Next string
}

func (a *Auth) sign(b []byte) []byte {
	m := hmac.New(sha256.New, a.key)
	m.Write(b)
	return m.Sum(nil)
}

func (a *Auth) seal(s loginState) string {
	b, _ := json.Marshal(s)
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(a.sign(b))
}

func (a *Auth) unseal(v string) (s loginState, ok bool) {
	p, m, found := strings.Cut(v, ".")
	b, err1 := base64.RawURLEncoding.DecodeString(p)
	mac, err2 := base64.RawURLEncoding.DecodeString(m)
	if !found || err1 != nil || err2 != nil || !hmac.Equal(mac, a.sign(b)) {
		return s, false
	}
	return s, json.Unmarshal(b, &s) == nil
}

func randStr() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// safeNext allows only same-site absolute paths.
func safeNext(n string) string {
	if n == "" || n[0] != '/' || strings.HasPrefix(n, "//") || strings.ContainsFunc(n, func(c rune) bool { return c < 0x21 || c == 0x7f || c == '\\' }) {
		return "/"
	}
	if u, err := url.Parse(n); err != nil || u.Scheme != "" || u.Host != "" {
		return "/"
	}
	return n
}

func (a *Auth) login(w http.ResponseWriter, r *http.Request) {
	s := loginState{State: randStr(), Nonce: randStr(), Verifier: oauth2.GenerateVerifier(), Next: safeNext(r.URL.Query().Get("next"))}
	setCookie(w, loginCookie, a.seal(s), 600)
	http.Redirect(w, r, a.oauth.AuthCodeURL(s.State, oauth2.S256ChallengeOption(s.Verifier), oidc.Nonce(s.Nonce)), http.StatusFound)
}

func groupsFrom(raw json.RawMessage) []string {
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return list
	}
	var one string
	if json.Unmarshal(raw, &one) == nil && one != "" {
		return []string{one}
	}
	return nil
}

func (a *Auth) audit(ctx context.Context, tx pgx.Tx, r *http.Request, subject any, action string, detail any) error {
	ip := any(nil)
	if addr := httpx.ClientIP(r, a.cfg.TrustedProxies); addr.IsValid() {
		ip = addr.String()
	}
	ua := httpx.UserAgent(r)
	d, _ := json.Marshal(detail)
	_, err := tx.Exec(ctx, `SELECT write_audit($1::uuid, $1::uuid, NULL, $2, $3::inet, $4, $5::jsonb)`, subject, action, ip, ua, d)
	return err
}

func (a *Auth) auditSystem(r *http.Request, subject any, action string, detail any) {
	// Best effort: a failed audit write must not turn a denial into a success.
	_ = a.db.WithSystem(r.Context(), func(tx pgx.Tx) error { return a.audit(r.Context(), tx, r, subject, action, detail) })
}

func (a *Auth) callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, err := r.Cookie(loginCookie)
	setCookie(w, loginCookie, "", -1) // single use, whatever happens next
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	s, ok := a.unseal(c.Value)
	q := r.URL.Query()
	if !ok || q.Get("code") == "" || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(s.State)) != 1 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	tok, err := a.oauth.Exchange(ctx, q.Get("code"), oauth2.VerifierOption(s.Verifier))
	if err != nil {
		http.Error(w, "login failed", http.StatusBadRequest)
		return
	}
	rawID, _ := tok.Extra("id_token").(string)
	idt, err := a.verifier.Verify(ctx, rawID)
	if err != nil || subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(s.Nonce)) != 1 {
		http.Error(w, "login failed", http.StatusBadRequest)
		return
	}
	var claims map[string]json.RawMessage
	var std struct{ Email, Name string }
	if idt.Claims(&claims) != nil || idt.Claims(&std) != nil {
		http.Error(w, "login failed", http.StatusBadRequest)
		return
	}
	groups := groupsFrom(claims[a.cfg.OIDCGroupsClaim])
	isAdmin := a.cfg.OIDCAdminGroup != "" && slices.Contains(groups, a.cfg.OIDCAdminGroup)
	if len(a.cfg.OIDCAllowedGroups) > 0 && !isAdmin && !slices.ContainsFunc(groups, func(g string) bool { return slices.Contains(a.cfg.OIDCAllowedGroups, g) }) {
		a.auditSystem(r, nil, "login_denied", map[string]string{"sub": idt.Subject, "reason": "allowlist"})
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}
	var userID string
	var disabled bool
	err = a.db.WithSystem(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT user_id::text, disabled FROM upsert_oidc_user($1,$2,$3,$4,$5)`,
			a.cfg.OIDCIssuer, idt.Subject, std.Email, std.Name, isAdmin).Scan(&userID, &disabled)
	})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if disabled {
		a.auditSystem(r, userID, "login_denied", map[string]string{"sub": idt.Subject, "reason": "disabled"})
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}
	sess, err := a.newSession(r, userID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	setCookie(w, CookieName, sess, int(sessionTTL.Seconds()))
	http.Redirect(w, r, safeNext(s.Next), http.StatusFound)
}

func (a *Auth) logout(w http.ResponseWriter, r *http.Request) {
	dest := "/"
	if a.endSession != "" {
		dest = a.endSession + "?" + url.Values{"post_logout_redirect_uri": {a.cfg.PublicURL.String() + "/"}, "client_id": {a.cfg.OIDCClientID}}.Encode()
	}
	p, id, ok, lerr := a.lookup(r)
	if lerr != nil { // cannot tell whether the session exists: keep the cookie so the user can retry
		slog.Error("logout lookup", "err", lerr)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	if ok {
		err := a.db.WithUser(r.Context(), p.UserID, func(tx pgx.Tx) error {
			if _, err := tx.Exec(r.Context(), `DELETE FROM web_sessions WHERE id_hash = $1`, id); err != nil {
				return err
			}
			return a.audit(r.Context(), tx, r, p.UserID, "logout", map[string]any{})
		})
		if err != nil { // keep the cookie so the user can retry
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	setCookie(w, CookieName, "", -1)
	http.Redirect(w, r, dest, http.StatusSeeOther)
}
