// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// AvatarFetch loads the image behind a stored picture URL: the body and its media type.
type AvatarFetch func(ctx context.Context, rawURL string) (body []byte, contentType string, err error)

const (
	avatarMax     = 512 << 10
	avatarTimeout = 5 * time.Second
	avatarTTL     = time.Hour
	avatarMissTTL = 5 * time.Minute // a failing IdP is asked again after this, not on every page load
)

var avatarTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true, "image/gif": true}

var errAvatar = errors.New("avatar refused")

// IssuerAvatarFetch fetches only https URLs on the issuer's origin (no SSRF to anything else), follows no redirects,
// and accepts at most 512 KiB of PNG, JPEG, WebP or GIF within 5 s. Errors never carry the URL, which may hold tokens.
func IssuerAvatarFetch(issuer string, rt http.RoundTripper) AvatarFetch {
	iss, err := url.Parse(issuer)
	client := &http.Client{Transport: rt, Timeout: avatarTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return func(ctx context.Context, raw string) ([]byte, string, error) {
		u, perr := url.Parse(raw)
		if err != nil || perr != nil || u.Scheme != "https" || iss.Scheme != "https" || u.User != nil ||
			!strings.EqualFold(u.Host, iss.Host) {
			return nil, "", fmt.Errorf("%w: not an https URL on the issuer's origin", errAvatar)
		}
		req, rerr := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if rerr != nil {
			return nil, "", errAvatar
		}
		resp, derr := client.Do(req)
		if derr != nil {
			return nil, "", fmt.Errorf("%w: fetch failed", errAvatar) // derr names the URL
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return nil, "", fmt.Errorf("%w: status %d", errAvatar, resp.StatusCode)
		}
		mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if !avatarTypes[mt] {
			return nil, "", fmt.Errorf("%w: content type", errAvatar)
		}
		if resp.ContentLength > avatarMax {
			return nil, "", fmt.Errorf("%w: too large", errAvatar)
		}
		body, berr := io.ReadAll(io.LimitReader(resp.Body, avatarMax+1))
		if berr != nil || len(body) > avatarMax {
			return nil, "", fmt.Errorf("%w: too large or cut off", errAvatar)
		}
		return body, mt, nil
	}
}

type avatarEntry struct {
	url   string
	body  []byte
	ctype string
	until time.Time
}

// avatarCache keeps one entry per user, keyed by user id and checked against the stored URL, so a new picture at login
// is fetched at once and one user's entry can never answer for another.
// shortcut: unbounded map with one entry (at most 512 KiB) per user who opened the dashboard; fine for a team-sized
// instance, upgrade to an LRU if instances grow to thousands of users.
type avatarCache struct {
	mu    sync.Mutex
	m     map[string]avatarEntry
	fetch AvatarFetch
	now   func() time.Time
}

func (c *avatarCache) get(ctx context.Context, userID, raw string) ([]byte, string, bool) {
	c.mu.Lock()
	e, ok := c.m[userID]
	c.mu.Unlock()
	if ok && e.url == raw && c.now().Before(e.until) {
		return e.body, e.ctype, e.body != nil
	}
	body, ctype, err := c.fetch(ctx, raw)
	e = avatarEntry{url: raw, until: c.now().Add(avatarMissTTL)}
	if err == nil {
		e = avatarEntry{url: raw, body: body, ctype: ctype, until: c.now().Add(avatarTTL)}
	}
	c.mu.Lock()
	c.m[userID] = e
	c.mu.Unlock()
	return e.body, e.ctype, e.body != nil
}

// avatar serves the signed-in user's own picture through the server, since the CSP allows images only from 'self'.
// No picture, a refused URL or any fetch failure is a 404; the UI then shows initials.
func (h *Handler) avatar(w http.ResponseWriter, r *http.Request) {
	p, _ := h.as(r)
	var pic *string
	err := h.db.WithUser(r.Context(), p.UserID, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `SELECT picture_url FROM users WHERE id = app_user()`).Scan(&pic)
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	if pic == nil {
		errJSON(w, http.StatusNotFound, "not_found")
		return
	}
	body, ctype, ok := h.avatars.get(r.Context(), p.UserID, *pic)
	if !ok {
		errJSON(w, http.StatusNotFound, "not_found")
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(body)
}
