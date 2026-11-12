// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package cli

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/usebraid/braid/internal/daemon/client"
	"github.com/usebraid/braid/internal/daemon/state"
	"github.com/usebraid/braid/internal/daemon/syncer"
	"github.com/usebraid/braid/internal/protocol"
)

type stubUp struct{ err error }

func (s stubUp) Ingest(context.Context, protocol.IngestRequest) (protocol.IngestResponse, error) {
	return protocol.IngestResponse{}, s.err
}
func (s stubUp) Files(context.Context) ([]protocol.RemoteFile, error) { return nil, s.err }

func TestTrackUpPicksUpNewTokenOn401(t *testing.T) {
	db, err := state.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	reloads := 0
	up := &trackUp{Uploader: stubUp{client.ErrUnauthorized}, db: db, reload: func() (syncer.Uploader, bool) {
		reloads++
		return stubUp{}, reloads == 1
	}}
	if _, err := up.Ingest(context.Background(), protocol.IngestRequest{}); err != nil {
		t.Fatalf("not retried with the new token: %v", err)
	}
	if _, err := up.Files(context.Background()); err != nil {
		t.Fatal(err)
	}
}
