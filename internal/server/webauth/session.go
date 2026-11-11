// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package webauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/usebraid/braid/internal/server/httpx"
)

// CookieName is the session cookie.
const CookieName = "__Host-braid_session"

const sessionTTL = 7 * 24 * time.Hour

// Principal is the authenticated web user.
type Principal struct {
	UserID  string
	IsAdmin bool
}

type ctxKey struct{}

// PrincipalFrom returns the principal set by Require.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}

func hashID(raw []byte) []byte { h := sha256.Sum256(raw); return h[:] }

func setCookie(w http.ResponseWriter, name, val string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: val, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
}

// newSession stores a fresh session for userID and returns the cookie value.
func (a *Auth) newSession(ctx context.Context, userID string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	now := a.now()
	err := a.db.WithUser(ctx, userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO web_sessions(id_hash, user_id, created_at, last_seen_at, expires_at)
			VALUES ($1, $2, $3, $3, $4)`, hashID(raw), userID, now, now.Add(sessionTTL))
		return err
	})
	return base64.RawURLEncoding.EncodeToString(raw), err
}

// lookup resolves the request's session cookie.
func (a *Auth) lookup(r *http.Request) (Principal, []byte, bool) {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return Principal{}, nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil || len(raw) != 32 {
		return Principal{}, nil, false
	}
	var p Principal
	err = a.db.WithSystem(r.Context(), func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `SELECT user_id::text, is_admin FROM auth_session($1)`, hashID(raw)).Scan(&p.UserID, &p.IsAdmin)
	})
	return p, hashID(raw), err == nil
}

// Require authenticates the request or answers 401 (API) / redirects to login.
func (a *Auth) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _, ok := a.lookup(r)
		if !ok {
			if len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
				httpx.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
				return
			}
			http.Redirect(w, r, "/auth/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, p)))
	})
}

// RequireAdmin must be used behind Require.
func (a *Auth) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p, ok := PrincipalFrom(r.Context()); !ok || !p.IsAdmin {
			httpx.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
