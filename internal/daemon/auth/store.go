// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/zalando/go-keyring"
)

// ErrNoToken means no token is stored for the server.
var ErrNoToken = errors.New("not logged in")

// ErrNoKeychain means the OS keychain backend is unavailable.
var ErrNoKeychain = errors.New("no OS keychain available")

// Store keeps one device token per server URL.
type Store interface {
	Get(server string) (string, error) // ErrNoToken if absent
	Set(server, token string) error
	Delete(server string) error
	Kind() string // "keychain" | "file"
}

const keyringService = "braid"

type keychain struct{}

// NewKeychain uses go-keyring service "braid", user = server URL; Set returns ErrNoKeychain when the backend is unavailable.
func NewKeychain() Store { return keychain{} }

func (keychain) Kind() string { return "keychain" }

func (keychain) Get(server string) (string, error) {
	v, err := keyring.Get(keyringService, server)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNoToken
	}
	return v, err
}

func (keychain) Set(server, token string) error {
	if err := keyring.Set(keyringService, server, token); err != nil {
		return fmt.Errorf("%w: %w", ErrNoKeychain, err)
	}
	return nil
}

func (keychain) Delete(server string) error {
	if err := keyring.Delete(keyringService, server); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return err
	}
	return nil
}

type fileStore struct{ dir string }

// NewFile stores tokens in <configDir>/token as JSON {server: token} with mode 0600 (dir 0700); refuses to read a file with group/other permission bits (unix).
func NewFile(configDir string) Store { return fileStore{configDir} }

func (f fileStore) Kind() string { return "file" }
func (f fileStore) path() string { return filepath.Join(f.dir, "token") }

func (f fileStore) load() (map[string]string, error) {
	p := f.path()
	fi, err := os.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("token file %s is accessible by others (mode %v); chmod 600 it or run braid login again", p, fi.Mode().Perm())
	}
	b, err := os.ReadFile(p) //nolint:gosec // daemon's own token file
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, errors.New("token file is corrupt")
	}
	return m, nil
}

func (f fileStore) save(m map[string]string) error {
	if err := os.MkdirAll(f.dir, 0o700); err != nil {
		return err
	}
	b, _ := json.Marshal(m)
	tmp, err := os.CreateTemp(f.dir, "token-*") // created 0600
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), f.path())
}

func (f fileStore) Get(server string) (string, error) {
	m, err := f.load()
	if err != nil {
		return "", err
	}
	if v, ok := m[server]; ok {
		return v, nil
	}
	return "", ErrNoToken
}

func (f fileStore) Set(server, token string) error {
	m, err := f.load()
	if err != nil {
		m = map[string]string{} // unreadable (e.g. loose permissions): overwrite with a fresh 0600 file
	}
	m[server] = token
	return f.save(m)
}

func (f fileStore) Delete(server string) error {
	m, err := f.load()
	if err != nil {
		return err
	}
	delete(m, server)
	return f.save(m)
}
