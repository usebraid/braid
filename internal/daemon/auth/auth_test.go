// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/usebraid/braid/internal/daemon/auth"
	"github.com/usebraid/braid/internal/protocol"
)

func TestLoginPollsUntilSuccess(t *testing.T) {
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/device/code":
			_ = json.NewEncoder(w).Encode(protocol.DeviceCodeResponse{DeviceCode: "dc", UserCode: "BCDF-GHJK", VerificationURIComplete: "u", ExpiresIn: 600, Interval: 5})
		case "/v1/device/token":
			polls++
			switch polls {
			case 1:
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
			case 2:
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"error":"slow_down"}`))
			default:
				_ = json.NewEncoder(w).Encode(protocol.TokenResponse{AccessToken: "braid_dev_x", TokenType: "Bearer", DeviceID: "d"}) //nolint:gosec // G117: test fixture
			}
		}
	}))
	defer srv.Close()
	var waits []time.Duration
	shown := ""
	tr, err := auth.Login(context.Background(), srv.Client(), srv.URL, protocol.DeviceCodeRequest{}, func(r protocol.DeviceCodeResponse) { shown = r.UserCode },
		func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil })
	if err != nil || tr.AccessToken != "braid_dev_x" || shown != "BCDF-GHJK" {
		t.Fatalf("%+v %v", tr, err)
	}
	if len(waits) != 3 || waits[0] != 5*time.Second || waits[2] != 10*time.Second {
		t.Fatalf("waits %v", waits)
	}
}

func TestLoginDeniedAndExpired(t *testing.T) {
	for status, want := range map[string]error{"access_denied": auth.ErrDenied, "expired_token": auth.ErrExpired} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/device/code" {
				_ = json.NewEncoder(w).Encode(protocol.DeviceCodeResponse{DeviceCode: "dc", Interval: 5, ExpiresIn: 600})
				return
			}
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"` + status + `"}`))
		}))
		_, err := auth.Login(context.Background(), srv.Client(), srv.URL, protocol.DeviceCodeRequest{}, func(protocol.DeviceCodeResponse) {}, func(context.Context, time.Duration) error { return nil })
		if !errors.Is(err, want) {
			t.Errorf("%s: %v", status, err)
		}
		srv.Close()
	}
}

func TestKeychainStore(t *testing.T) {
	keyring.MockInit()
	s := auth.NewKeychain()
	if _, err := s.Get("https://x"); !errors.Is(err, auth.ErrNoToken) {
		t.Fatal(err)
	}
	_ = s.Set("https://x", "braid_dev_a")
	if v, _ := s.Get("https://x"); v != "braid_dev_a" {
		t.Fatal(v)
	}
	_ = s.Delete("https://x")
	if _, err := s.Get("https://x"); !errors.Is(err, auth.ErrNoToken) {
		t.Fatal(err)
	}
}

func TestFileStorePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions")
	}
	dir := filepath.Join(t.TempDir(), "cfg")
	s := auth.NewFile(dir)
	if err := s.Set("https://x", "braid_dev_a"); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(filepath.Join(dir, "token"))
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	_ = os.Chmod(filepath.Join(dir, "token"), 0o644) //nolint:gosec // G302: deliberately loosened to test rejection
	if _, err := s.Get("https://x"); err == nil {
		t.Fatal("world-readable token file accepted")
	}
}
