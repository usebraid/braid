// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package api

import (
	"net/http"
	"strings"

	"github.com/usebraid/braid/internal/buildinfo"
	"github.com/usebraid/braid/internal/server/config"
	"github.com/usebraid/braid/internal/server/httpx"
)

// About describes the running build.
type About struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	SourceURL string `json:"source_url"` // link to exactly this version
}

// SourceLink returns the URL of the source of the given build below base.
func SourceLink(base, version, commit string) string {
	switch {
	case commit != "":
		return base + "/tree/" + commit
	case version != "dev":
		return base + "/releases/tag/v" + strings.TrimPrefix(version, "v")
	}
	return base
}

func (h *Handler) about(w http.ResponseWriter, _ *http.Request) {
	base := h.cfg.SourceURL
	if base == "" {
		base = config.DefaultSourceURL
	}
	v, c := buildinfo.Info()
	httpx.WriteJSON(w, http.StatusOK, About{Version: v, Commit: c, SourceURL: SourceLink(base, v, c)})
}
