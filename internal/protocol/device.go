// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package protocol

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// DeviceCodeRequest is the body of POST /v1/device/code.
type DeviceCodeRequest struct {
	Hostname      string `json:"hostname"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	OSUser        string `json:"os_user"`
	DaemonVersion string `json:"daemon_version"`
}

// Validate requires each field to be 1..128 bytes of valid UTF-8 without control characters.
func (r DeviceCodeRequest) Validate() error {
	for name, v := range map[string]string{
		"hostname": r.Hostname, "os": r.OS, "arch": r.Arch,
		"os_user": r.OSUser, "daemon_version": r.DaemonVersion,
	} {
		if len(v) < 1 || len(v) > 128 || !utf8.ValidString(v) || strings.ContainsFunc(v, unicode.IsControl) {
			return fmt.Errorf("protocol: invalid %s", name)
		}
	}
	return nil
}

// DeviceCodeResponse is the RFC 8628 device authorization response.
type DeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// GrantTypeDeviceCode is the RFC 8628 grant type.
const GrantTypeDeviceCode = "urn:ietf:params:oauth:grant-type:device_code"

// TokenRequest is the body of POST /v1/device/token.
type TokenRequest struct {
	GrantType  string `json:"grant_type"` // must be GrantTypeDeviceCode
	DeviceCode string `json:"device_code"`
}

// TokenResponse is the successful device token response.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	DeviceID    string `json:"device_id"`
}

// OAuthError is an RFC 8628 error body; Error is one of authorization_pending|slow_down|expired_token|access_denied|invalid_grant|invalid_request.
type OAuthError struct {
	Error string `json:"error"`
}

// UserCodeAlphabet is the character set of user codes (no vowels, no look-alikes).
const UserCodeAlphabet = "BCDFGHJKLMNPQRSTVWXZ"

// TokenPrefix prefixes every device token.
//
//nolint:gosec // G101: public prefix, not a credential
const TokenPrefix = "braid_dev_"

// NormalizeUserCode uppercases, strips spaces and '-', and requires exactly 8 alphabet characters.
func NormalizeUserCode(s string) (string, error) {
	s = strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(s))
	if len(s) != 8 || strings.Trim(s, UserCodeAlphabet) != "" {
		return "", errors.New("protocol: invalid user code")
	}
	return s, nil
}
