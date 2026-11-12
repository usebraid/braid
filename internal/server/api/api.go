// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package api serves the /api/v1 endpoints used by the web app.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/usebraid/braid/internal/protocol"
	"github.com/usebraid/braid/internal/server/config"
	"github.com/usebraid/braid/internal/server/db"
	"github.com/usebraid/braid/internal/server/httpx"
	"github.com/usebraid/braid/internal/server/webauth"
)

// Me is the current user.
type Me struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	IsAdmin bool   `json:"is_admin"`
}

// Device is one registered device.
type Device struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Hostname      string     `json:"hostname"`
	OS            string     `json:"os"`
	Arch          string     `json:"arch"`
	OSUser        string     `json:"os_user"`
	DaemonVersion string     `json:"daemon_version"`
	CreatedAt     time.Time  `json:"created_at"`
	LastSeenAt    *time.Time `json:"last_seen_at"`
	LastSyncAt    *time.Time `json:"last_sync_at"`
	RevokedAt     *time.Time `json:"revoked_at"`
	Stale         bool       `json:"stale"`
}

// AuditEntry is one audit log row.
type AuditEntry struct {
	ID            int64           `json:"id"`
	At            time.Time       `json:"at"`
	Action        string          `json:"action"`
	ActorUserID   *string         `json:"actor_user_id"`
	SubjectUserID *string         `json:"subject_user_id"`
	DeviceID      *string         `json:"device_id"`
	IP            *string         `json:"ip"`
	UserAgent     *string         `json:"user_agent"`
	Detail        json.RawMessage `json:"detail"`
}

// AuditPage is one page of audit entries; Next is the cursor for the following page.
type AuditPage struct {
	Entries []AuditEntry `json:"entries"`
	Next    *int64       `json:"next"`
}

// AdminUser is a user as seen by an admin.
type AdminUser struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	Name        string     `json:"name"`
	IsAdmin     bool       `json:"is_admin"`
	DisabledAt  *time.Time `json:"disabled_at"`
	LastLoginAt *time.Time `json:"last_login_at"`
	LastSyncAt  *time.Time `json:"last_sync_at"`
	Devices     int        `json:"devices"`
	StoredBytes int64      `json:"stored_bytes"`
}

// Handler serves /api/v1.
type Handler struct {
	cfg config.Config
	db  *db.DB
}

// New returns a Handler.
func New(cfg config.Config, d *db.DB) *Handler { return &Handler{cfg: cfg, db: d} }

// Register mounts the routes behind auth.
func (h *Handler) Register(mux *http.ServeMux, auth *webauth.Auth) {
	user := func(f http.HandlerFunc) http.Handler { return auth.Require(f) }
	mut := func(f http.HandlerFunc) http.Handler {
		return httpx.RequireSameOrigin(h.cfg.PublicURL, auth.Require(f))
	}
	admin := func(f http.HandlerFunc) http.Handler { return auth.Require(auth.RequireAdmin(f)) }
	adminMut := func(f http.HandlerFunc) http.Handler {
		return httpx.RequireSameOrigin(h.cfg.PublicURL, auth.Require(auth.RequireAdmin(f)))
	}
	mux.Handle("GET /api/v1/me", user(h.me))
	mux.Handle("GET /api/v1/devices", user(h.devices))
	mux.Handle("PATCH /api/v1/devices/{id}", mut(h.rename))
	mux.Handle("DELETE /api/v1/devices/{id}", mut(h.revoke))
	mux.Handle("GET /api/v1/audit", user(h.audit))
	mux.Handle("GET /api/v1/admin/users", admin(h.adminUsers))
	mux.Handle("POST /api/v1/admin/users/{id}/disable", adminMut(h.setDisabled(true)))
	mux.Handle("POST /api/v1/admin/users/{id}/enable", adminMut(h.setDisabled(false)))
	mux.Handle("DELETE /api/v1/admin/users/{id}", adminMut(h.deleteUser))
	mux.Handle("GET /api/v1/admin/audit", admin(h.adminAudit))
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func errJSON(w http.ResponseWriter, status int, code string) {
	httpx.WriteJSON(w, status, map[string]string{"error": code})
}

var errNotFound = errors.New("not found")

// fail maps an error to a response.
func fail(w http.ResponseWriter, r *http.Request, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, errNotFound):
		errJSON(w, http.StatusNotFound, "not_found")
	case errors.As(err, &pg) && pg.Code == "42501":
		errJSON(w, http.StatusForbidden, "forbidden")
	case errors.As(err, &pg) && pg.Code == "P02000": // no_data_found
		errJSON(w, http.StatusNotFound, "not_found")
	case errors.As(err, &pg) && pg.Code == "P0001": // guard exceptions (own account)
		errJSON(w, http.StatusBadRequest, "invalid_request")
	default:
		slog.Error("api error", "method", r.Method, "path", r.URL.Path, "err", err)
		errJSON(w, http.StatusInternalServerError, "internal")
	}
}

func (h *Handler) as(r *http.Request) (webauth.Principal, bool) {
	p, ok := webauth.PrincipalFrom(r.Context())
	return p, ok
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	p, _ := h.as(r)
	var m Me
	err := h.db.WithUser(r.Context(), p.UserID, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `SELECT id::text, email, name FROM users WHERE id = app_user()`).Scan(&m.ID, &m.Email, &m.Name)
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	m.IsAdmin = p.IsAdmin
	httpx.WriteJSON(w, http.StatusOK, m)
}

const deviceCols = `id::text, name, coalesce(hostname,''), coalesce(os,''), coalesce(arch,''), coalesce(os_user,''),
  coalesce(daemon_version,''), created_at, last_seen_at, last_sync_at, revoked_at,
  (revoked_at IS NULL AND coalesce(last_sync_at, created_at) < now() - interval '7 days')`

func scanDevice(row pgx.Row) (d Device, err error) {
	err = row.Scan(&d.ID, &d.Name, &d.Hostname, &d.OS, &d.Arch, &d.OSUser, &d.DaemonVersion,
		&d.CreatedAt, &d.LastSeenAt, &d.LastSyncAt, &d.RevokedAt, &d.Stale)
	return
}

func (h *Handler) devices(w http.ResponseWriter, r *http.Request) {
	p, _ := h.as(r)
	out := []Device{}
	err := h.db.WithUser(r.Context(), p.UserID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT `+deviceCols+` FROM devices ORDER BY created_at, id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			d, err := scanDevice(rows)
			if err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) ipUA(r *http.Request) (ip any, ua string) {
	if a := httpx.ClientIP(r, h.cfg.TrustedProxies); a.IsValid() {
		ip = a.String()
	}
	ua = r.UserAgent()
	if len(ua) > 256 {
		ua = strings.ToValidUTF8(ua[:256], "")
	}
	return
}

func (h *Handler) auditDevice(ctx context.Context, tx pgx.Tx, r *http.Request, uid, id, action string, detail any) error {
	ip, ua := h.ipUA(r)
	d, _ := json.Marshal(detail)
	_, err := tx.Exec(ctx, `SELECT write_audit($1::uuid, $1::uuid, $2::uuid, $3, $4::inet, $5, $6::jsonb)`, uid, id, action, ip, ua, d)
	return err
}

func (h *Handler) rename(w http.ResponseWriter, r *http.Request) {
	p, _ := h.as(r)
	id := r.PathValue("id")
	if !uuidRe.MatchString(id) {
		errJSON(w, http.StatusNotFound, "not_found")
		return
	}
	var body struct {
		Name *string `json:"name"`
	}
	if err := httpx.ReadJSON(r, 4096, &body); err != nil || body.Name == nil {
		errJSON(w, http.StatusBadRequest, "invalid_request")
		return
	}
	name := strings.TrimSpace(*body.Name)
	if !protocol.ValidText(name, 64) {
		errJSON(w, http.StatusBadRequest, "invalid_name")
		return
	}
	var d Device
	err := h.db.WithUser(r.Context(), p.UserID, func(tx pgx.Tx) error {
		var old string
		// FOR UPDATE subquery keeps the old name consistent for the audit detail.
		err := tx.QueryRow(r.Context(), `SELECT name FROM devices WHERE id = $1 FOR UPDATE`, id).Scan(&old)
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotFound
		} else if err != nil {
			return err
		}
		d, err = scanDevice(tx.QueryRow(r.Context(), `UPDATE devices SET name = $2 WHERE id = $1 RETURNING `+deviceCols, id, name))
		if err != nil {
			return err
		}
		return h.auditDevice(r.Context(), tx, r, p.UserID, id, "device_renamed", map[string]string{"from": old, "to": name})
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, d)
}

func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	p, _ := h.as(r)
	id := r.PathValue("id")
	if !uuidRe.MatchString(id) {
		errJSON(w, http.StatusNotFound, "not_found")
		return
	}
	err := h.db.WithUser(r.Context(), p.UserID, func(tx pgx.Tx) error {
		var got string
		err := tx.QueryRow(r.Context(), `UPDATE devices SET revoked_at = coalesce(revoked_at, now()) WHERE id = $1 RETURNING id::text`, id).Scan(&got)
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotFound
		} else if err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `UPDATE device_tokens SET revoked_at = now() WHERE device_id = $1 AND revoked_at IS NULL`, id); err != nil {
			return err
		}
		return h.auditDevice(r.Context(), tx, r, p.UserID, id, "device_revoked", nil)
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func auditPage(r *http.Request, rows pgx.Rows) (AuditPage, error) {
	defer rows.Close()
	page := AuditPage{Entries: []AuditEntry{}}
	for rows.Next() {
		var e AuditEntry
		var detail []byte
		if err := rows.Scan(&e.ID, &e.At, &e.Action, &e.ActorUserID, &e.SubjectUserID, &e.DeviceID, &e.IP, &e.UserAgent, &detail); err != nil {
			return page, err
		}
		e.Detail = detail
		page.Entries = append(page.Entries, e)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Entries) == limitOf(r) {
		next := page.Entries[len(page.Entries)-1].ID
		page.Next = &next
	}
	return page, nil
}

func limitOf(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil {
		return 50
	}
	return max(1, min(n, 200))
}

const auditCols = `id, at, action, actor_user_id::text, subject_user_id::text, device_id::text, host(ip), user_agent, detail`

func (h *Handler) auditHandler(own bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := h.as(r)
		var before *int64
		if s := r.URL.Query().Get("before"); s != "" {
			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				errJSON(w, http.StatusBadRequest, "invalid_request")
				return
			}
			before = &n
		}
		q := `SELECT ` + auditCols + ` FROM audit_log WHERE ($1::bigint IS NULL OR id < $1) ORDER BY id DESC LIMIT $2`
		if !own {
			q = `SELECT ` + auditCols + ` FROM admin_audit($1, $2)`
		}
		var page AuditPage
		err := h.db.WithUser(r.Context(), p.UserID, func(tx pgx.Tx) error {
			rows, err := tx.Query(r.Context(), q, before, limitOf(r))
			if err != nil {
				return err
			}
			page, err = auditPage(r, rows)
			return err
		})
		if err != nil {
			fail(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, page)
	}
}

func (h *Handler) audit(w http.ResponseWriter, r *http.Request)      { h.auditHandler(true)(w, r) }
func (h *Handler) adminAudit(w http.ResponseWriter, r *http.Request) { h.auditHandler(false)(w, r) }

func (h *Handler) listUsers(ctx context.Context, tx pgx.Tx) ([]AdminUser, error) {
	rows, err := tx.Query(ctx, `SELECT id::text, email, name, is_admin, disabled_at, last_login_at, last_sync_at, devices, stored_bytes FROM admin_list_users()`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminUser{}
	for rows.Next() {
		var u AdminUser
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.IsAdmin, &u.DisabledAt, &u.LastLoginAt, &u.LastSyncAt, &u.Devices, &u.StoredBytes); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (h *Handler) adminUsers(w http.ResponseWriter, r *http.Request) {
	p, _ := h.as(r)
	var out []AdminUser
	err := h.db.WithUser(r.Context(), p.UserID, func(tx pgx.Tx) (err error) {
		out, err = h.listUsers(r.Context(), tx)
		return
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) setDisabled(disabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := h.as(r)
		id := r.PathValue("id")
		if !uuidRe.MatchString(id) {
			errJSON(w, http.StatusNotFound, "not_found")
			return
		}
		err := h.db.WithUser(r.Context(), p.UserID, func(tx pgx.Tx) error {
			_, err := tx.Exec(r.Context(), `SELECT admin_set_disabled($1::uuid, $2)`, id, disabled)
			return err
		})
		if err != nil {
			fail(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	p, _ := h.as(r)
	id := r.PathValue("id")
	if !uuidRe.MatchString(id) {
		errJSON(w, http.StatusNotFound, "not_found")
		return
	}
	confirm := r.URL.Query().Get("confirm")
	err := h.db.WithUser(r.Context(), p.UserID, func(tx pgx.Tx) error {
		users, err := h.listUsers(r.Context(), tx)
		if err != nil {
			return err
		}
		for _, u := range users {
			if strings.EqualFold(u.ID, id) {
				want := u.Email
				if want == "" {
					want = u.ID // no email on file: the id is the confirmation
				}
				if confirm == "" || !strings.EqualFold(confirm, want) {
					return errConfirm
				}
				_, err := tx.Exec(r.Context(), `SELECT admin_delete_user($1::uuid)`, id)
				return err
			}
		}
		return errNotFound
	})
	if errors.Is(err, errConfirm) {
		errJSON(w, http.StatusBadRequest, "confirm_mismatch")
		return
	} else if err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var errConfirm = errors.New("confirm mismatch")
