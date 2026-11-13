// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package device implements the RFC 8628 device authorization flow and its /device pages.
package device

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/usebraid/braid/internal/protocol"
	"github.com/usebraid/braid/internal/server/config"
	"github.com/usebraid/braid/internal/server/db"
	"github.com/usebraid/braid/internal/server/httpx"
	"github.com/usebraid/braid/internal/server/ingest"
	"github.com/usebraid/braid/internal/server/page"
	"github.com/usebraid/braid/internal/server/webauth"
)

// Handler serves the device endpoints.
type Handler struct {
	cfg       config.Config
	db        *db.DB
	code      *httpx.Limiter // POST /v1/device/code per IP
	entrySess *httpx.Limiter // wrong codes per session
	entryIP   *httpx.Limiter // wrong codes per IP
	revoke    *httpx.Limiter
}

// New returns a Handler.
func New(cfg config.Config, d *db.DB) *Handler {
	return &Handler{cfg: cfg, db: d, code: httpx.NewLimiter(10, time.Now),
		entrySess: httpx.NewLimiter(5, time.Now), entryIP: httpx.NewLimiter(20, time.Now), revoke: httpx.NewLimiter(600, time.Now)}
}

// Register mounts the device routes.
func (h *Handler) Register(mux *http.ServeMux, auth *webauth.Auth) {
	mux.HandleFunc("POST /v1/device/code", h.requestCode)
	mux.HandleFunc("POST /v1/device/token", h.token)
	mux.Handle("POST /v1/device/revoke", ingest.RequireDevice(h.db, "ingest:write", h.revoke, http.HandlerFunc(h.revokeToken)))
	mux.Handle("GET /device", auth.Require(http.HandlerFunc(h.entry)))
	mux.Handle("POST /device/approve", httpx.RequireSameOrigin(h.cfg.PublicURL, auth.Require(http.HandlerFunc(h.decide(true)))))
	mux.Handle("POST /device/deny", httpx.RequireSameOrigin(h.cfg.PublicURL, auth.Require(http.HandlerFunc(h.decide(false)))))
}

func sum(s string) []byte { h := sha256.Sum256([]byte(s)); return h[:] }

func (h *Handler) ip(r *http.Request) string {
	if a := httpx.ClientIP(r, h.cfg.TrustedProxies); a.IsValid() {
		return a.String()
	}
	return ""
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// audit is best effort: a failed audit write must not change the outcome of the request.
func (h *Handler) audit(ctx context.Context, r *http.Request, subject any, action string, detail any) {
	ua := httpx.UserAgent(r)
	d, _ := json.Marshal(detail)
	err := h.db.WithSystem(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT write_audit($1::uuid, $1::uuid, NULL, $2, $3::inet, $4, $5::jsonb)`, subject, action, nilIfEmpty(h.ip(r)), ua, d)
		return err
	})
	if err != nil {
		slog.Error("audit write failed", "action", action, "err", err)
	}
}

func newUserCode() (string, error) {
	const limit = 256 - 256%len(protocol.UserCodeAlphabet) // rejection sampling: no modulo bias
	out := make([]byte, 0, 8)
	var buf [32]byte
	for len(out) < 8 {
		if _, err := rand.Read(buf[:]); err != nil {
			return "", err
		}
		for _, b := range buf {
			if int(b) < limit && len(out) < 8 {
				out = append(out, protocol.UserCodeAlphabet[int(b)%len(protocol.UserCodeAlphabet)])
			}
		}
	}
	return string(out), nil
}

func (h *Handler) requestCode(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ip := h.ip(r)
	if !h.code.Allow(ip) {
		httpx.TooMany(w, time.Minute)
		return
	}
	var req protocol.DeviceCodeRequest
	if err := httpx.ReadJSON(r, 4096, &req); err != nil || req.Validate() != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, protocol.OAuthError{Error: "invalid_request"})
		return
	}
	info := map[string]string{"hostname": req.Hostname, "os": req.OS, "arch": req.Arch, "os_user": req.OSUser, "daemon_version": req.DaemonVersion}
	infoJSON, _ := json.Marshal(info)
	var dc, uc string
	var err error
	for range 3 { // a user-code collision violates the unique index; draw again
		raw := make([]byte, 32)
		if _, err = rand.Read(raw); err != nil {
			break
		}
		dc = base64.RawURLEncoding.EncodeToString(raw)
		if uc, err = newUserCode(); err != nil {
			break
		}
		err = h.db.WithSystem(r.Context(), func(tx pgx.Tx) error {
			_, e := tx.Exec(r.Context(), `SELECT device_code_create($1, $2, $3::jsonb, $4::inet)`, sum(dc), sum(uc), infoJSON, nilIfEmpty(ip))
			return e
		})
		var pe *pgconn.PgError
		if err == nil || !errors.As(err, &pe) || pe.Code != "23505" {
			break
		}
	}
	if err != nil {
		slog.Error("device code create", "err", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, protocol.OAuthError{Error: "server_error"})
		return
	}
	h.audit(r.Context(), r, nil, "device_code_requested", map[string]any{"client": info, "ip": ip})
	base := h.cfg.PublicURL.String() + "/device"
	httpx.WriteJSON(w, http.StatusOK, protocol.DeviceCodeResponse{DeviceCode: dc, UserCode: uc[:4] + "-" + uc[4:],
		VerificationURI: base, VerificationURIComplete: base + "?user_code=" + uc[:4] + "-" + uc[4:], ExpiresIn: 600, Interval: 5})
}

func (h *Handler) token(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var req protocol.TokenRequest
	if err := httpx.ReadJSON(r, 4096, &req); err != nil || req.GrantType != protocol.GrantTypeDeviceCode || req.DeviceCode == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, protocol.OAuthError{Error: "invalid_request"})
		return
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, protocol.OAuthError{Error: "server_error"})
		return
	}
	tok := protocol.TokenPrefix + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw))
	var status string
	var devID *string
	err := h.db.WithSystem(r.Context(), func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `SELECT status, device_id::text FROM device_code_poll($1, $2)`, sum(req.DeviceCode), sum(tok)).Scan(&status, &devID)
	})
	if err != nil {
		slog.Error("device poll", "err", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, protocol.OAuthError{Error: "server_error"})
		return
	}
	if status != "ok" || devID == nil {
		httpx.WriteJSON(w, http.StatusBadRequest, protocol.OAuthError{Error: status})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, protocol.TokenResponse{AccessToken: tok, TokenType: "Bearer", Scope: "ingest:write stats:read:self", DeviceID: *devID})
}

func (h *Handler) revokeToken(w http.ResponseWriter, r *http.Request) {
	tok, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ") // RequireDevice validated it
	var ok bool
	err := h.db.WithSystem(r.Context(), func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `SELECT device_token_revoke($1)`, sum(tok)).Scan(&ok)
	})
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, protocol.OAuthError{Error: "server_error"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type info struct{ Hostname, DefaultName, OS, Arch, OSUser, IP, Requested, DaemonVersion string }

type view = page.View

func (h *Handler) page(w http.ResponseWriter, status int, v view) { page.Render(w, status, v) }

func sessionKey(r *http.Request) string {
	c, err := r.Cookie(webauth.CookieName)
	if err != nil {
		return ""
	}
	return hex.EncodeToString(sum(c.Value))
}

// limited consumes one token from both limiters before any lookup, so parallel guesses cannot outrun the limit.
func (h *Handler) limited(r *http.Request) bool {
	okSess := h.entrySess.Allow(sessionKey(r))
	okIP := h.entryIP.Allow(h.ip(r))
	return !okSess || !okIP
}

// miss audits a well-formed code that matched no pending row (the limit was consumed up front).
func (h *Handler) miss(r *http.Request, p webauth.Principal) {
	h.audit(r.Context(), r, p.UserID, "device_code_failed", map[string]any{})
}

func tooMany(w http.ResponseWriter, h *Handler) {
	w.Header().Set("Retry-After", "60")
	h.page(w, http.StatusTooManyRequests, view{View: "none", Title: "Too many attempts", Error: "Too many wrong codes. Try again in a minute."})
}

func (h *Handler) entry(w http.ResponseWriter, r *http.Request) {
	p, _ := webauth.PrincipalFrom(r.Context())
	raw := r.URL.Query().Get("user_code")
	if raw == "" {
		h.page(w, http.StatusOK, view{View: "form", Title: "Connect a device"})
		return
	}
	code, err := protocol.NormalizeUserCode(raw)
	if err != nil { // malformed input is not a guess; it is not counted
		h.page(w, http.StatusOK, view{View: "form", Title: "Connect a device", UserCode: truncate(raw), Error: "Enter the 8-character code shown in your terminal."})
		return
	}
	if h.limited(r) {
		tooMany(w, h)
		return
	}
	var in info
	var ci map[string]string
	var ip *string
	var created time.Time
	found := true
	err = h.db.WithSystem(r.Context(), func(tx pgx.Tx) error {
		e := tx.QueryRow(r.Context(), `SELECT client_info, host(ip), created_at FROM device_code_lookup($1)`, sum(code)).Scan(&ci, &ip, &created)
		if errors.Is(e, pgx.ErrNoRows) {
			found = false
			return nil
		}
		return e
	})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	disp := code[:4] + "-" + code[4:]
	if !found {
		h.miss(r, p)
		h.page(w, http.StatusNotFound, view{View: "form", Title: "Connect a device", UserCode: disp, Error: "Code not found or expired."})
		return
	}
	in = info{Hostname: ci["hostname"], OS: ci["os"], Arch: ci["arch"], OSUser: ci["os_user"],
		DefaultName: firstRunes(ci["hostname"], 64), Requested: created.UTC().Format(time.RFC3339), DaemonVersion: ci["daemon_version"]}
	if ip != nil {
		in.IP = *ip
	}
	h.page(w, http.StatusOK, view{View: "confirm", Title: "Approve this device?", UserCode: disp, Info: in})
}

func firstRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

func truncate(s string) string {
	if len(s) > 32 {
		return ""
	}
	return s
}

func validName(s string) bool {
	return protocol.ValidText(s, 64)
}

func (h *Handler) decide(approve bool) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := webauth.PrincipalFrom(r.Context())
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := r.ParseForm(); err != nil {
			h.page(w, http.StatusBadRequest, view{View: "none", Title: "Error", Error: "Bad request."})
			return
		}
		code, err := protocol.NormalizeUserCode(r.PostForm.Get("user_code"))
		name := strings.TrimSpace(r.PostForm.Get("name"))
		if err != nil || (approve && !validName(name)) {
			h.page(w, http.StatusBadRequest, view{View: "none", Title: "Error", Error: "Invalid code or device name."})
			return
		}
		if h.limited(r) {
			tooMany(w, h)
			return
		}
		var id *string
		err = h.db.WithUser(r.Context(), p.UserID, func(tx pgx.Tx) error {
			return tx.QueryRow(r.Context(), `SELECT device_code_decide($1, $2, $3)::text`, sum(code), approve, name).Scan(&id)
		})
		if err != nil {
			slog.Error("device decide", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		switch {
		case id == nil:
			h.miss(r, p)
			h.page(w, http.StatusBadRequest, view{View: "none", Title: "Error", Error: "Code expired or already used."})
		case approve:
			h.page(w, http.StatusOK, view{View: "none", Title: "Device approved", Body: "Your terminal will continue now. You can close this tab."})
		default:
			h.page(w, http.StatusOK, view{View: "none", Title: "Denied", Body: "The device was not connected. You can close this tab."})
		}
	}
}
