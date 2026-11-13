// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package api_test

import (
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/usebraid/braid/internal/protocol"
	srvapi "github.com/usebraid/braid/internal/server/api"
	"github.com/usebraid/braid/internal/server/stats"
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
	for _, p := range []string{"/api/v1/me", "/api/v1/me/avatar", "/api/v1/devices", "/api/v1/devices/{id}", "/api/v1/audit", "/api/v1/admin/users",
		"/api/v1/admin/users/{id}/disable", "/api/v1/admin/users/{id}/enable", "/api/v1/admin/users/{id}", "/api/v1/admin/audit",
		"/api/v1/stats/overview", "/api/v1/stats/sessions", "/api/v1/stats/sessions/{id}", "/api/v1/stats/sessions/{id}/lines",
		"/api/v1/stats/failures", "/api/v1/stats/search", "/api/v1/admin/stats", "/v1/stats/self", "/v1/files"} {
		if !have["  "+p+":"] {
			t.Errorf("missing path %s", p)
		}
	}
}

func schemaProps(t *testing.T, doc, name string) []string {
	t.Helper()
	lines := strings.Split(doc, "\n")
	in, props := false, false
	var out []string
	key := regexp.MustCompile(`^        ([a-z_0-9]+):`)
	for _, l := range lines {
		switch {
		case l == "    "+name+":":
			in, props = true, false
		case in && strings.HasPrefix(l, "    ") && !strings.HasPrefix(l, "     "):
			in = false
		case in && l == "      properties:":
			props = true
		case in && props:
			if m := key.FindStringSubmatch(l); m != nil {
				out = append(out, m[1])
			}
		}
	}
	sort.Strings(out)
	return out
}

func jsonFields(typ reflect.Type) []string {
	var out []string
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		switch {
		case f.Anonymous && tag == "":
			out = append(out, jsonFields(f.Type)...)
		case tag != "" && tag != "-":
			out = append(out, tag)
		}
	}
	sort.Strings(out)
	return out
}

func TestSchemasMatchGoTypes(t *testing.T) {
	b, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	for name, v := range map[string]any{
		"Me": srvapi.Me{}, "Overview": stats.Overview{}, "KPI": stats.KPI{}, "DayPoint": stats.DayPoint{}, "ModelPoint": stats.ModelPoint{},
		"ProjectPoint": stats.ProjectPoint{}, "HeatCell": stats.HeatCell{}, "SessionItem": stats.SessionItem{}, "SessionPage": stats.SessionPage{},
		"AgentItem": stats.AgentItem{}, "AgentUsage": stats.AgentUsage{}, "PhaseItem": stats.PhaseItem{}, "FailureItem": stats.FailureItem{},
		"SessionDetail": stats.SessionDetail{}, "LineItem": stats.LineItem{}, "LinePage": stats.LinePage{}, "FailureAgg": stats.FailureAgg{},
		"FailurePage": stats.FailurePage{}, "SearchHit": stats.SearchHit{}, "SearchResult": stats.SearchResult{}, "SelfToday": stats.SelfToday{},
		"SelfSession": stats.SelfSession{}, "SelfLimit": stats.SelfLimit{}, "Self": stats.Self{}, "AdminUserStats": srvapi.AdminUserStats{}, "AdminTotals": srvapi.AdminTotals{},
		"AdminStats": srvapi.AdminStats{}, "RemoteFile": protocol.RemoteFile{}, "FilesPage": protocol.FilesPage{},
	} {
		got, want := schemaProps(t, doc, name), jsonFields(reflect.TypeOf(v))
		if !reflect.DeepEqual(got, want) {
			t.Errorf("schema %s: yaml %v, go %v", name, got, want)
		}
	}
	for _, p := range []string{"/api/v1/stats/overview", "/api/v1/stats/sessions", "/api/v1/stats/sessions/{id}", "/api/v1/stats/sessions/{id}/lines",
		"/api/v1/stats/failures", "/api/v1/stats/search", "/api/v1/admin/stats", "/v1/stats/self", "/v1/files"} {
		if !strings.Contains(doc, "\n  "+p+":\n") {
			t.Errorf("missing path %s", p)
		}
	}
	if !strings.Contains(doc, "operationId: patchMe") || !strings.Contains(doc, "device:") {
		t.Error("patchMe or the device security scheme is missing")
	}
	ids := regexp.MustCompile(`operationId: (\w+)`).FindAllStringSubmatch(doc, -1)
	seen := map[string]bool{}
	for _, m := range ids {
		if seen[m[1]] {
			t.Errorf("duplicate operationId %s", m[1])
		}
		seen[m[1]] = true
	}
}
