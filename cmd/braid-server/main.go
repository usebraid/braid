// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Command braid-server is the Braid server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
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
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "braid-server:", err)
		if errors.Is(err, errUsage) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

const usage = "usage: braid-server [version | reindex [--user <uuid>]]  (no argument starts the server)"

var errUsage = errors.New(usage)

func run(args []string) error {
	args = append([]string{"braid-server"}, args...)
	switch {
	case len(args) == 1, args[1] == "version", args[1] == "--version", args[1] == "-v", args[1] == "reindex":
	default:
		return errUsage
	}
	if len(args) > 2 && args[1] != "reindex" {
		return errUsage
	}
	if len(args) > 1 && (args[1] == "version" || args[1] == "--version" || args[1] == "-v") {
		v, c := buildinfo.Info()
		if c == "" {
			c = "unknown"
		}
		fmt.Printf("braid-server %s (%s)\n", v, c)
		return nil
	}
	var user *string
	if len(args) > 1 && args[1] == "reindex" { // flags are checked before the environment is read
		fs := flag.NewFlagSet("reindex", flag.ContinueOnError)
		user = fs.String("user", "", "reindex only this user id (default: all)")
		fs.SetOutput(io.Discard)
		if err := fs.Parse(args[2:]); err != nil || fs.NArg() > 0 {
			return errUsage
		}
		if *user != "" {
			if !uuidRe.MatchString(*user) {
				return errors.New("--user: not a uuid")
			}
		}
	}
	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if user != nil {
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
