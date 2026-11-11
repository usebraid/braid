// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package testdb gives each test its own freshly migrated Postgres database.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/usebraid/braid/internal/server/db"
)

// New creates database braid_test_<random> on BRAID_TEST_DATABASE_URL (a superuser URL), ensures the
// cluster roles exist, migrates as braid_owner and drops the database on cleanup.
func New(t testing.TB) (ownerURL, appURL string) {
	t.Helper()
	adminURL := os.Getenv("BRAID_TEST_DATABASE_URL")
	if adminURL == "" {
		t.Fatal("BRAID_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	defer func() { _ = admin.Close(ctx) }()

	// Session lock so concurrent test packages do not race on CREATE ROLE.
	for _, q := range []string{
		`SELECT pg_advisory_lock(4243)`,
		`DO $$ BEGIN
		   IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'braid_owner') THEN
		     CREATE ROLE braid_owner LOGIN PASSWORD 'owner' BYPASSRLS;
		   END IF;
		   IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'braid_app') THEN
		     CREATE ROLE braid_app LOGIN PASSWORD 'app' NOBYPASSRLS;
		   END IF;
		   ALTER ROLE braid_owner BYPASSRLS;
		   ALTER ROLE braid_app NOBYPASSRLS;
		 END $$`,
		`SELECT pg_advisory_unlock(4243)`,
	} {
		if _, err := admin.Exec(ctx, q); err != nil {
			t.Fatalf("roles: %v", err)
		}
	}

	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	name := "braid_test_" + hex.EncodeToString(b[:])
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+name+` OWNER braid_owner`); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(ctx, adminURL)
		if err != nil {
			t.Errorf("cleanup connect: %v", err)
			return
		}
		defer func() { _ = c.Close(ctx) }()
		if _, err := c.Exec(ctx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); err != nil {
			t.Errorf("drop database: %v", err)
		}
	})

	u, err := url.Parse(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	u.User = url.UserPassword("braid_owner", "owner")
	ownerURL = u.String()
	u.User = url.UserPassword("braid_app", "app")
	appURL = u.String()

	if err := db.Migrate(ctx, ownerURL); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return ownerURL, appURL
}

// Exec runs SQL on the owner connection (for backdating rows in tests).
func Exec(t testing.TB, ownerURL, sql string, args ...any) {
	t.Helper()
	ctx := context.Background()
	c, err := pgx.Connect(ctx, ownerURL)
	if err != nil {
		t.Fatalf("connect owner: %v", err)
	}
	defer func() { _ = c.Close(ctx) }()
	if _, err := c.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec: %v", err)
	}
}
