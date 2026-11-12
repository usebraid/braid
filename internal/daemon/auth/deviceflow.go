// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package auth implements the device-flow client and token storage.
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/usebraid/braid/internal/protocol"
)

// ErrDenied is returned when the user denied the device in the browser.
var ErrDenied = errors.New("device was denied in the browser")

// ErrExpired is returned when the device code expired.
var ErrExpired = errors.New("code expired, run braid login again")

func post(ctx context.Context, hc *http.Client, url string, in any) (int, []byte, error) {
	b, err := json.Marshal(in)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, data, err
}

// Login runs the device flow: POST code, call show(resp), poll every interval (+5 s on slow_down) until
// success, access_denied, expired_token or ctx end. Response bodies are never included in errors.
func Login(ctx context.Context, hc *http.Client, serverURL string, info protocol.DeviceCodeRequest, show func(protocol.DeviceCodeResponse), sleep func(context.Context, time.Duration) error) (protocol.TokenResponse, error) {
	base := strings.TrimRight(serverURL, "/")
	st, body, err := post(ctx, hc, base+"/v1/device/code", info)
	if err != nil {
		return protocol.TokenResponse{}, fmt.Errorf("request device code: %w", err)
	}
	var dc protocol.DeviceCodeResponse
	if st != http.StatusOK || json.Unmarshal(body, &dc) != nil || dc.DeviceCode == "" {
		return protocol.TokenResponse{}, fmt.Errorf("request device code: server answered %d", st)
	}
	show(dc)
	interval := time.Duration(dc.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	for {
		if err := sleep(ctx, interval); err != nil {
			return protocol.TokenResponse{}, err
		}
		st, body, err := post(ctx, hc, base+"/v1/device/token", protocol.TokenRequest{GrantType: protocol.GrantTypeDeviceCode, DeviceCode: dc.DeviceCode})
		if err != nil {
			if ctx.Err() != nil {
				return protocol.TokenResponse{}, ctx.Err()
			}
			continue // transient network error: keep polling until the code expires
		}
		if st == http.StatusOK {
			var tr protocol.TokenResponse
			if json.Unmarshal(body, &tr) != nil || tr.AccessToken == "" {
				return protocol.TokenResponse{}, errors.New("malformed token response")
			}
			return tr, nil
		}
		var oe protocol.OAuthError
		_ = json.Unmarshal(body, &oe)
		switch oe.Error {
		case "authorization_pending":
		case "slow_down":
			interval += 5 * time.Second
		case "access_denied":
			return protocol.TokenResponse{}, ErrDenied
		case "expired_token":
			return protocol.TokenResponse{}, ErrExpired
		default:
			return protocol.TokenResponse{}, fmt.Errorf("token request failed: %d %q", st, oe.Error)
		}
	}
}
