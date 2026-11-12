// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package server wires the HTTP handlers of braid-server.
package server

import (
	"context"
	"net/http"
	"time"

	"github.com/usebraid/braid/internal/server/api"
	"github.com/usebraid/braid/internal/server/config"
	"github.com/usebraid/braid/internal/server/db"
	"github.com/usebraid/braid/internal/server/device"
	"github.com/usebraid/braid/internal/server/httpx"
	"github.com/usebraid/braid/internal/server/ingest"
	"github.com/usebraid/braid/internal/server/webauth"
)

// Server owns the DB pool and the route table.
type Server struct {
	db  *db.DB
	mux *http.ServeMux
}

// New opens the database (as braid_app) and registers all routes.
func New(ctx context.Context, cfg config.Config) (*Server, error) {
	d, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	auth, err := webauth.New(ctx, cfg, d, time.Now)
	if err != nil {
		d.Close()
		return nil, err
	}
	mux := http.NewServeMux()
	auth.Register(mux)
	device.New(cfg, d).Register(mux, auth)
	ingest.New(d).Register(mux, httpx.NewLimiter(600, time.Now))
	api.New(cfg, d).Register(mux, auth)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		pctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if d.Ping(pctx) != nil {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	return &Server{db: d, mux: mux}, nil
}

// Handler returns the mux wrapped in the security headers.
func (s *Server) Handler() http.Handler { return httpx.SecurityHeaders(s.mux) }

// Close releases the DB pool.
func (s *Server) Close() { s.db.Close() }
