// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package webauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log/slog"
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

// newSession stores a fresh session and its login audit row in one transaction and returns the cookie value.
func (a *Auth) newSession(r *http.Request, userID string) (string, error) {
	ctx := r.Context()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	now := a.now()
	err := a.db.WithUser(ctx, userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO web_sessions(id_hash, user_id, created_at, last_seen_at, expires_at)
			VALUES ($1, $2, $3, $3, $4)`, hashID(raw), userID, now, now.Add(sessionTTL))
		if err != nil {
			return err
		}
		return a.audit(ctx, tx, r, userID, "login", map[string]any{})
	})
	return base64.RawURLEncoding.EncodeToString(raw), err
}

// lookup resolves the request's session cookie and slides its expiry. ok is false without a valid
// session; err is set only when the lookup itself failed (database error), which is not "no session".
func (a *Auth) lookup(r *http.Request) (p Principal, id []byte, ok bool, err error) {
	return a.resolve(r, "auth_session")
}

// Check validates the session cookie without side effects (no sliding expiry); see lookup for ok and err.
func (a *Auth) Check(r *http.Request) (Principal, bool, error) {
	p, _, ok, err := a.resolve(r, "auth_session_peek")
	return p, ok, err
}

func (a *Auth) resolve(r *http.Request, fn string) (p Principal, id []byte, ok bool, err error) {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return Principal{}, nil, false, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil || len(raw) != 32 {
		return Principal{}, nil, false, nil
	}
	err = a.db.WithSystem(r.Context(), func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `SELECT user_id::text, is_admin FROM `+fn+`($1)`, hashID(raw)).Scan(&p.UserID, &p.IsAdmin)
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Principal{}, hashID(raw), false, nil
	case err != nil:
		return Principal{}, hashID(raw), false, err
	}
	return p, hashID(raw), true, nil
}

// Require authenticates the request or answers 401 (API) / redirects to login.
func (a *Auth) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _, ok, err := a.lookup(r)
		if err != nil { // a database error says nothing about the session: do not log the user out
			slog.Error("session lookup", "err", err)
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "unavailable"})
			return
		}
		if !ok {
			if len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
				httpx.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
				return
			}
			http.Redirect(w, r, "/auth/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
			return
		}
		if c, err := r.Cookie(CookieName); err == nil { // sliding expiry: keep the browser cookie as long-lived as the DB row
			setCookie(w, CookieName, c.Value, int(sessionTTL.Seconds()))
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
