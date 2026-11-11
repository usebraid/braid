// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package config parses the server's BRAID_* environment.
package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
)

// Config is the parsed server configuration.
type Config struct {
	PublicURL         *url.URL
	Listen            string
	DatabaseURL       string
	DatabaseOwnerURL  string
	OIDCIssuer        string
	OIDCClientID      string
	OIDCClientSecret  string
	OIDCScopes        []string
	OIDCGroupsClaim   string
	OIDCAdminGroup    string
	OIDCAllowedGroups []string
	TrustedProxies    []netip.Prefix
}

func splitList(s, sep string) []string {
	var out []string
	for _, p := range strings.Split(s, sep) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parsePublicURL(s string) (*url.URL, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, err
	}
	local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"
	switch {
	case u.Host == "" || u.Opaque != "":
		return nil, errors.New("must be an absolute URL")
	case u.Scheme != "https" && (u.Scheme != "http" || !local):
		return nil, errors.New("must be https (http only for localhost)")
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.User != nil:
		return nil, errors.New("must not contain userinfo, query or fragment")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u, nil
}

// FromEnv reads the BRAID_* variables via getenv and reports every missing or invalid one.
func FromEnv(getenv func(string) string) (Config, error) {
	var errs []error
	req := func(k string) string {
		v := getenv(k)
		if v == "" {
			errs = append(errs, fmt.Errorf("%s: required", k))
		}
		return v
	}
	def := func(k, d string) string {
		if v := getenv(k); v != "" {
			return v
		}
		return d
	}
	var c Config
	if s := req("BRAID_PUBLIC_URL"); s != "" {
		u, err := parsePublicURL(s)
		if err != nil {
			errs = append(errs, fmt.Errorf("BRAID_PUBLIC_URL: %w", err))
		}
		c.PublicURL = u
	}
	c.Listen = def("BRAID_LISTEN", ":8080")
	c.DatabaseURL = req("BRAID_DATABASE_URL")
	c.DatabaseOwnerURL = req("BRAID_DATABASE_OWNER_URL")
	c.OIDCIssuer = req("BRAID_OIDC_ISSUER")
	c.OIDCClientID = req("BRAID_OIDC_CLIENT_ID")
	c.OIDCClientSecret = req("BRAID_OIDC_CLIENT_SECRET")
	c.OIDCScopes = strings.Fields(def("BRAID_OIDC_SCOPES", "openid profile email groups"))
	c.OIDCGroupsClaim = def("BRAID_OIDC_GROUPS_CLAIM", "groups")
	c.OIDCAdminGroup = def("BRAID_OIDC_ADMIN_GROUP", "braid-admins")
	c.OIDCAllowedGroups = splitList(getenv("BRAID_OIDC_ALLOWED_GROUPS"), ",")
	for _, p := range splitList(getenv("BRAID_TRUSTED_PROXIES"), ",") {
		pf, err := netip.ParsePrefix(p)
		if err != nil {
			errs = append(errs, fmt.Errorf("BRAID_TRUSTED_PROXIES: %w", err))
			continue
		}
		c.TrustedProxies = append(c.TrustedProxies, pf.Masked())
	}
	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}
	return c, nil
}
