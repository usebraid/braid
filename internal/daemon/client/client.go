// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package client is the daemon's HTTP client for the Braid server.
package client

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/usebraid/braid/internal/protocol"
)

const maxResponse = 1 << 20

// ErrUnauthorized means the server rejected the device token (401).
var ErrUnauthorized = errors.New("unauthorized")

// ProtocolMismatchError is returned on 426.
type ProtocolMismatchError struct{ Server, Min int }

func (e *ProtocolMismatchError) Error() string {
	return fmt.Sprintf("server speaks protocol %d (min %d), this braid speaks %d — update braid", e.Server, e.Min, protocol.Version)
}

// RetryableError covers network errors, 408, 429 and 5xx.
type RetryableError struct {
	Status     int
	RetryAfter time.Duration
	Err        error
}

func (e *RetryableError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("retryable (status %d): %v", e.Status, e.Err)
	}
	return fmt.Sprintf("retryable (status %d)", e.Status)
}

func (e *RetryableError) Unwrap() error { return e.Err }

// PermanentError is any other non-success status.
type PermanentError struct {
	Status int
	Body   string
}

func (e *PermanentError) Error() string {
	return fmt.Sprintf("server rejected request (status %d): %s", e.Status, e.Body)
}

// Client talks to the Braid server.
type Client struct {
	base, token, ua string
	hc              *http.Client
}

// New builds a client; a nil hc gets a 120 s timeout.
func New(baseURL, token, version string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 120 * time.Second}
	}
	return &Client{base: strings.TrimRight(baseURL, "/"), token: token, ua: "braid/" + version, hc: hc}
}

// Ingest uploads a batch of file changes.
func (c *Client) Ingest(ctx context.Context, req protocol.IngestRequest) (protocol.IngestResponse, error) {
	var out protocol.IngestResponse
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if err := json.NewEncoder(zw).Encode(req); err != nil {
		return out, err
	}
	if err := zw.Close(); err != nil {
		return out, err
	}
	body, err := c.do(ctx, http.MethodPost, "/v1/ingest", &buf, true)
	if err != nil {
		return out, err
	}
	return out, decode(body, &out)
}

// Files lists the files the server knows for this device.
func (c *Client) Files(ctx context.Context) ([]protocol.RemoteFile, error) {
	body, err := c.do(ctx, http.MethodGet, "/v1/files", nil, false)
	if err != nil {
		return nil, err
	}
	var out []protocol.RemoteFile
	return out, decode(body, &out)
}

// Revoke revokes this device; already revoked (401) counts as done.
func (c *Client) Revoke(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodPost, "/v1/device/revoke", nil, false)
	if errors.Is(err, ErrUnauthorized) {
		return nil
	}
	return err
}

func decode(body []byte, v any) error {
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, gz bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set(protocol.HeaderVersion, strconv.Itoa(protocol.Version))
	req.Header.Set("User-Agent", c.ua)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if gz {
		req.Header.Set("Content-Encoding", "gzip")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// url.Error never contains headers, so the token cannot leak.
		return nil, &RetryableError{Err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return nil, &RetryableError{Status: resp.StatusCode, Err: err}
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return data, nil
	}
	return nil, classify(resp, data)
}

func classify(resp *http.Response, data []byte) error {
	var er protocol.ErrorResponse
	_ = json.Unmarshal(data, &er)
	switch s := resp.StatusCode; {
	case s == http.StatusUnauthorized:
		return ErrUnauthorized
	case s == http.StatusUpgradeRequired:
		return &ProtocolMismatchError{Server: er.Server, Min: er.Min}
	case s == http.StatusTooManyRequests, s == http.StatusRequestTimeout, s >= 500:
		var ra time.Duration
		if n, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); err == nil && n > 0 {
			ra = time.Duration(n) * time.Second
		}
		return &RetryableError{Status: s, RetryAfter: ra}
	default:
		msg := er.Error
		if msg == "" {
			msg = http.StatusText(s)
		}
		return &PermanentError{Status: s, Body: msg}
	}
}
