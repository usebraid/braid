// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func base() map[string]string {
	return map[string]string{
		"BRAID_PUBLIC_URL":         "https://braid.example.org/",
		"BRAID_DATABASE_URL":       "postgres://app@db/braid",
		"BRAID_DATABASE_OWNER_URL": "postgres://owner@db/braid",
		"BRAID_OIDC_ISSUER":        "https://id.example.org",
		"BRAID_OIDC_CLIENT_ID":     "braid",
		"BRAID_OIDC_CLIENT_SECRET": "s",
	}
}

func TestDefaults(t *testing.T) {
	c, err := FromEnv(env(base()))
	if err != nil {
		t.Fatal(err)
	}
	if c.PublicURL.String() != "https://braid.example.org" {
		t.Errorf("public url %s", c.PublicURL)
	}
	if c.Listen != ":8080" || c.OIDCGroupsClaim != "groups" || c.OIDCAdminGroup != "braid-admins" {
		t.Errorf("%+v", c)
	}
	if strings.Join(c.OIDCScopes, " ") != "openid profile email groups" {
		t.Errorf("scopes %v", c.OIDCScopes)
	}
	if len(c.OIDCAllowedGroups) != 0 || len(c.TrustedProxies) != 0 {
		t.Errorf("%+v", c)
	}
}

func TestParsesListsAndProxies(t *testing.T) {
	m := base()
	m["BRAID_OIDC_ALLOWED_GROUPS"] = "braid-users, friends"
	m["BRAID_TRUSTED_PROXIES"] = "10.0.0.0/8,172.16.0.1/32"
	c, err := FromEnv(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.OIDCAllowedGroups) != 2 || c.OIDCAllowedGroups[1] != "friends" {
		t.Errorf("%v", c.OIDCAllowedGroups)
	}
	if len(c.TrustedProxies) != 2 {
		t.Errorf("%v", c.TrustedProxies)
	}
}

func TestRejectsMissingAndInvalid(t *testing.T) {
	_, err := FromEnv(env(map[string]string{}))
	if err == nil {
		t.Fatal("empty env accepted")
	}
	for _, k := range []string{"BRAID_PUBLIC_URL", "BRAID_DATABASE_URL", "BRAID_DATABASE_OWNER_URL", "BRAID_OIDC_ISSUER", "BRAID_OIDC_CLIENT_ID", "BRAID_OIDC_CLIENT_SECRET"} {
		if !strings.Contains(err.Error(), k) {
			t.Errorf("error does not name %s", k)
		}
	}
	for _, u := range []string{"http://braid.example.org", "braid.example.org", "https://x/?a=1", "https://x/#f", "https://x/?"} {
		m := base()
		m["BRAID_PUBLIC_URL"] = u
		if _, err := FromEnv(env(m)); err == nil {
			t.Errorf("%s accepted", u)
		}
	}
	m := base()
	m["BRAID_PUBLIC_URL"] = "http://localhost:8080"
	if _, err := FromEnv(env(m)); err != nil {
		t.Errorf("localhost http rejected: %v", err)
	}
	m = base()
	m["BRAID_TRUSTED_PROXIES"] = "nope"
	if _, err := FromEnv(env(m)); err == nil {
		t.Error("bad CIDR accepted")
	}
}

func TestJoinsAllErrors(t *testing.T) {
	m := base()
	m["BRAID_PUBLIC_URL"] = "http://x.example"
	m["BRAID_TRUSTED_PROXIES"] = "nope"
	delete(m, "BRAID_OIDC_ISSUER")
	_, err := FromEnv(env(m))
	if err == nil {
		t.Fatal("accepted")
	}
	for _, k := range []string{"BRAID_PUBLIC_URL", "BRAID_TRUSTED_PROXIES", "BRAID_OIDC_ISSUER"} {
		if !strings.Contains(err.Error(), k) {
			t.Errorf("missing %s in %v", k, err)
		}
	}
}
