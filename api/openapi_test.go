// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package api_test

import (
	"os"
	"strings"
	"testing"
)

func TestOpenAPIPaths(t *testing.T) {
	b, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	if lines[0] != "openapi: 3.1.0" {
		t.Fatalf("first line %q", lines[0])
	}
	have := map[string]bool{}
	for _, l := range lines {
		have[l] = true
	}
	for _, p := range []string{"/api/v1/me", "/api/v1/devices", "/api/v1/devices/{id}", "/api/v1/audit", "/api/v1/admin/users",
		"/api/v1/admin/users/{id}/disable", "/api/v1/admin/users/{id}/enable", "/api/v1/admin/users/{id}", "/api/v1/admin/audit"} {
		if !have["  "+p+":"] {
			t.Errorf("missing path %s", p)
		}
	}
}
