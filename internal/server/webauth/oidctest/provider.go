// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package oidctest is a minimal in-process OIDC provider for tests.
package oidctest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// User is the identity the provider logs in.
type User struct {
	Sub, Email, Name string
	Groups           []string // nil → claim omitted
	Picture          string   // "" → claim omitted
}

// Provider is a running mock provider.
type Provider struct {
	Issuer string // httptest server URL
	// Tamper switches for negative tests.
	WrongNonce, WrongAudience, WrongIssuer, ExpiredToken bool

	clientID, secret string
	key              *rsa.PrivateKey
	mu               sync.Mutex
	user             User
	codes            map[string]authCode
}

type authCode struct {
	nonce, challenge, redirect string
	user                       User
}

// New starts a provider; it is stopped when t finishes.
func New(t testing.TB, clientID, clientSecret string) *Provider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &Provider{clientID: clientID, secret: clientSecret, key: key, codes: map[string]authCode{}}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	p.Issuer = srv.URL
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": p.Issuer, "authorization_endpoint": p.Issuer + "/authorize", "token_endpoint": p.Issuer + "/token",
			"jwks_uri": p.Issuer + "/jwks", "end_session_endpoint": p.Issuer + "/end_session",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("GET /authorize", p.authorize)
	mux.HandleFunc("POST /token", p.token)
	mux.HandleFunc("GET /end_session", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	return p
}

// SetUser sets the identity returned by later logins.
func (p *Provider) SetUser(u User) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.user = u
}

func (p *Provider) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != p.clientID || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("redirect_uri") == "" {
		http.Error(w, "bad request", 400)
		return
	}
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	code := base64.RawURLEncoding.EncodeToString(raw)
	p.mu.Lock()
	p.codes[code] = authCode{nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri"), user: p.user}
	p.mu.Unlock()
	u, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		http.Error(w, "bad redirect_uri", 400)
		return
	}
	v := u.Query()
	v.Set("code", code)
	v.Set("state", q.Get("state"))
	u.RawQuery = v.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound) //nolint:gosec // test provider; redirects to the caller's redirect_uri by design
}

func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	id, sec, ok := r.BasicAuth()
	if !ok {
		id, sec = r.PostFormValue("client_id"), r.PostFormValue("client_secret")
	} else if id, ok = unescape(id); ok {
		sec, ok = unescape(sec)
	}
	if !ok || id != p.clientID || subtle.ConstantTimeCompare([]byte(sec), []byte(p.secret)) != 1 {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
		return
	}
	p.mu.Lock()
	ac, found := p.codes[r.PostFormValue("code")]
	delete(p.codes, r.PostFormValue("code")) // single use
	p.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostFormValue("code_verifier")))
	if !found || r.PostFormValue("redirect_uri") != ac.redirect ||
		subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(ac.challenge)) != 1 {
		http.Error(w, `{"error":"invalid_grant"}`, 400)
		return
	}
	idt, err := p.idToken(ac)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 300, "id_token": idt})
}

func unescape(s string) (string, bool) {
	u, err := url.QueryUnescape(s)
	return u, err == nil
}

func (p *Provider) idToken(ac authCode) (string, error) {
	iss, aud, nonce, exp := p.Issuer, p.clientID, ac.nonce, time.Now().Add(time.Hour)
	if p.WrongIssuer {
		iss = "https://evil.example"
	}
	if p.WrongAudience {
		aud = "someone-else"
	}
	if p.WrongNonce {
		nonce = "wrong"
	}
	if p.ExpiredToken {
		exp = time.Now().Add(-time.Hour)
	}
	claims := map[string]any{"iss": iss, "aud": aud, "sub": ac.user.Sub, "nonce": nonce, "email": ac.user.Email,
		"name": ac.user.Name, "iat": time.Now().Add(-2 * time.Hour).Unix(), "exp": exp.Unix()}
	if ac.user.Groups != nil {
		claims["groups"] = ac.user.Groups
	}
	if ac.user.Picture != "" {
		claims["picture"] = ac.user.Picture
	}
	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: p.key, KeyID: "k1"}},
		(&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		return "", err
	}
	return jwt.Signed(sig).Claims(claims).Serialize()
}
