// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Command braid-server is the Braid server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"github.com/usebraid/braid/internal/buildinfo"
	"github.com/usebraid/braid/internal/server"
	"github.com/usebraid/braid/internal/server/config"
	"github.com/usebraid/braid/internal/server/db"
)

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "braid-server:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version" || os.Args[1] == "-v") {
		v, c := buildinfo.Info()
		if c == "" {
			c = "unknown"
		}
		fmt.Printf("braid-server %s (%s)\n", v, c)
		return nil
	}
	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(os.Args) > 1 && os.Args[1] == "reindex" {
		fs := flag.NewFlagSet("reindex", flag.ContinueOnError)
		user := fs.String("user", "", "reindex only this user id (default: all)")
		if err := fs.Parse(os.Args[2:]); err != nil {
			return err
		}
		if *user != "" {
			if !uuidRe.MatchString(*user) {
				return errors.New("--user: not a uuid")
			}
		}
		if err := db.Migrate(ctx, cfg.DatabaseOwnerURL); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
		return server.Reindex(ctx, cfg, *user)
	}
	if err := db.Migrate(ctx, cfg.DatabaseOwnerURL); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	s, err := server.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer s.Close()
	// ReadTimeout also bounds slow-body clients that would otherwise hold ingest slots.
	srv := &http.Server{Addr: cfg.Listen, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second}
	errc := make(chan error, 1)
	go func() {
		if err := s.RunWorker(ctx); err != nil {
			errc <- err
		}
	}()
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("listening", "addr", cfg.Listen)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(sctx)
}
