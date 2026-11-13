// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

//go:build docs

package db_test

import (
	"context"
	"os"
	"testing"

	"github.com/usebraid/braid/internal/server/db"
)

// TestMigrateExternal runs the real migrations as the owner role created by the external-Postgres docs SQL
// (scripts/test-docs-sql.sh sets BRAID_DOCS_OWNER_URL).
func TestMigrateExternal(t *testing.T) {
	u := os.Getenv("BRAID_DOCS_OWNER_URL")
	if u == "" {
		t.Skip("BRAID_DOCS_OWNER_URL not set")
	}
	if err := db.Migrate(context.Background(), u); err != nil {
		t.Fatal(err)
	}
}
