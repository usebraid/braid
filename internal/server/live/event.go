// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package live fans per-user events from Postgres NOTIFY out to WebSocket clients.
package live

// NOTIFY channels carrying Event payloads.
const (
	ChannelDerived = "braid_derived"
	ChannelSynced  = "braid_synced"
)

// Event is the NOTIFY payload; Message is what clients receive. It never carries data, only ids.
type Event struct {
	UserID    string `json:"u"`
	Type      string `json:"t"` // "session_updated" | "reindex" | "resync" on braid_derived; "device_synced" on braid_synced
	SessionID string `json:"s,omitempty"`
	DeviceID  string `json:"d,omitempty"`
	State     string `json:"state,omitempty"` // reindex: "started" | "done"
}
