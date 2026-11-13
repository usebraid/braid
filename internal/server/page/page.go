// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package page renders the server-side HTML pages (device login, login errors) in the dashboard's look.
package page

import (
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"html/template"
	"net/http"
)

//go:embed templates/shell.html
var tmplFS embed.FS

var tmpl = template.Must(template.ParseFS(tmplFS, "templates/shell.html"))

// View is the data of one page. View selects the body ("form", "confirm", else none); Info is the confirm view's
// device details; Retry adds a link back to "/" labelled RetryLabel.
type View struct {
	Nonce, View, Title, Body, Error, UserCode, RetryLabel string
	Retry                                                 bool
	Info                                                  any
}

// Render writes v with the given status and a CSP that allows only the nonce'd style block and no assets.
func Render(w http.ResponseWriter, status int, v View) {
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	v.Nonce = base64.RawURLEncoding.EncodeToString(nonce)
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, v); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	hd := w.Header()
	hd.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'; style-src 'nonce-"+v.Nonce+"'")
	hd.Set("Content-Type", "text/html; charset=utf-8")
	hd.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}
