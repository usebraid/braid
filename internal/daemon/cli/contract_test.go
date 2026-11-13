// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/usebraid/braid/internal/daemon/cli"
)

const bandDir = "../../../band"

func schema(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(bandDir, "stats.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func tags(typ reflect.Type) (all, required []string) {
	for i := 0; i < typ.NumField(); i++ {
		parts := strings.Split(typ.Field(i).Tag.Get("json"), ",")
		all = append(all, parts[0])
		if !slices.Contains(parts[1:], "omitempty") {
			required = append(required, parts[0])
		}
	}
	sort.Strings(all)
	sort.Strings(required)
	return
}

func keys(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func reqs(m map[string]any) []string {
	var out []string
	for _, v := range m["required"].([]any) {
		out = append(out, v.(string))
	}
	sort.Strings(out)
	return out
}

func TestSchemaMatchesGoTypes(t *testing.T) {
	root := schema(t)
	props := root["properties"].(map[string]any)
	for name, typ := range map[string]reflect.Type{
		"": reflect.TypeOf(cli.StatsOut{}), "sync": reflect.TypeOf(cli.StatsSync{}), "today": reflect.TypeOf(cli.StatsToday{}),
		"session": reflect.TypeOf(cli.StatsSession{}), "limit": reflect.TypeOf(cli.StatsLimit{}),
	} {
		obj := root
		if name != "" {
			obj = props[name].(map[string]any)
		}
		all, req := tags(typ)
		if !reflect.DeepEqual(keys(obj["properties"].(map[string]any)), all) || !reflect.DeepEqual(reqs(obj), req) {
			t.Errorf("%q: schema %v/%v, go %v/%v", name, keys(obj["properties"].(map[string]any)), reqs(obj), all, req)
		}
		if obj["additionalProperties"] != false {
			t.Errorf("%q: additionalProperties must be false", name)
		}
	}
	var enum []string
	for _, v := range props["sync"].(map[string]any)["properties"].(map[string]any)["state"].(map[string]any)["enum"].([]any) {
		enum = append(enum, v.(string))
	}
	want := []string{cli.StateOK, cli.StateBacklog, cli.StateBlocked, cli.StateOffline, cli.StateLoggedOut}
	sort.Strings(enum)
	sort.Strings(want)
	if !reflect.DeepEqual(enum, want) {
		t.Errorf("state enum %v, go %v", enum, want)
	}
	if props["version"].(map[string]any)["const"] != float64(cli.StatsVersion) {
		t.Error("version const differs from cli.StatsVersion")
	}
}

// Every fixture decodes strictly into the Go type (no unknown fields) and round-trips to the same JSON,
// so a fixture can only drift from the Go output shape by failing here.
func TestFixturesRoundTripThroughGoTypes(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(bandDir, "testdata", "stats-*.json"))
	if len(files) != 5 {
		t.Fatalf("fixtures: %v", files)
	}
	for _, f := range files {
		b, _ := os.ReadFile(f) //nolint:gosec // G304: path comes from a glob over our own testdata
		var s cli.StatsOut
		dec := json.NewDecoder(strings.NewReader(string(b)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&s); err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		back, _ := json.Marshal(s)
		var a, c any
		_ = json.Unmarshal(b, &a)
		_ = json.Unmarshal(back, &c)
		if !reflect.DeepEqual(a, c) {
			t.Errorf("%s: round trip %s", f, back)
		}
	}
}

// The real command output is one of the fixture shapes: same key sets.
func TestCommandOutputKeysMatchFixture(t *testing.T) {
	r := newRig(t, okHandler)
	_, raw, _ := r.stats(t, "--json", "--session", "s1")
	var got, want map[string]any
	_ = json.Unmarshal([]byte(raw), &got)
	b, _ := os.ReadFile(filepath.Join(bandDir, "testdata", "stats-ok.json"))
	_ = json.Unmarshal(b, &want)
	for _, k := range []string{"", "sync", "today", "session", "limit"} {
		g, w := got, want
		if k != "" {
			g, _ = got[k].(map[string]any), 0
			w = want[k].(map[string]any)
		}
		if !reflect.DeepEqual(keys(g), keys(w)) {
			t.Errorf("%q: %v vs %v", k, keys(g), keys(w))
		}
	}
}
