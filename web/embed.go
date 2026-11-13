// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package web embeds the built web app (web/dist).
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the built web app rooted at its index.html.
func Dist() fs.FS {
	f, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // "dist" is embedded above, so Sub cannot fail
	}
	return f
}
