// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package ingest serves the device-authenticated data path: POST /v1/ingest and GET /v1/files.
package ingest

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/usebraid/braid/internal/protocol"
	"github.com/usebraid/braid/internal/server/db"
	"github.com/usebraid/braid/internal/server/httpx"
)

// Device is the authenticated caller; every field comes from the token row, never from the request.
type Device struct {
	TokenID, DeviceID, UserID string
	Scopes                    []string
}

type ctxKey struct{}

// DeviceFrom returns the device placed in ctx by RequireDevice.
func DeviceFrom(ctx context.Context) (Device, bool) {
	d, ok := ctx.Value(ctxKey{}).(Device)
	return d, ok
}

func errJSON(w http.ResponseWriter, status int, code string) {
	httpx.WriteJSON(w, status, protocol.ErrorResponse{Error: code})
}

// RequireDevice authenticates a device token, then checks scope, protocol version and rate limit (in that order).
func RequireDevice(d *db.DB, scope string, lim *httpx.Limiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || !strings.HasPrefix(tok, protocol.TokenPrefix) {
			errJSON(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		hash := sha256.Sum256([]byte(tok))
		var dev Device
		found := false
		err := d.WithSystem(r.Context(), func(tx pgx.Tx) error {
			err := tx.QueryRow(r.Context(), `SELECT token_id::text, device_id::text, user_id::text, scopes FROM auth_device_token($1)`,
				hash[:]).Scan(&dev.TokenID, &dev.DeviceID, &dev.UserID, &dev.Scopes)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			found = err == nil
			return err
		})
		if err != nil {
			errJSON(w, http.StatusInternalServerError, "internal")
			return
		}
		if !found {
			errJSON(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if !slices.Contains(dev.Scopes, scope) {
			errJSON(w, http.StatusForbidden, "insufficient_scope")
			return
		}
		if r.Header.Get(protocol.HeaderVersion) != "1" {
			httpx.WriteJSON(w, http.StatusUpgradeRequired, protocol.ErrorResponse{Error: "protocol_mismatch", Server: protocol.Version, Min: 1})
			return
		}
		if !lim.Allow(dev.DeviceID) {
			httpx.TooMany(w, time.Second)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, dev)))
	})
}
