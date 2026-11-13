// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/usebraid/braid/internal/daemon/client"
)

// StatsVersion is the version of the `braid stats --json` contract (band/stats.schema.json).
const StatsVersion = 1

// StatsTTL is how long a fetched answer is reused.
const StatsTTL = 30 * time.Second

// Sync states of StatsSync.State.
const (
	StateOK        = "ok"
	StateBacklog   = "backlog"
	StateBlocked   = "blocked"
	StateOffline   = "offline"
	StateLoggedOut = "logged_out"
)

// StatsOut is the output of `braid stats --json`.
type StatsOut struct {
	Version int           `json:"version"`
	Sync    StatsSync     `json:"sync"`
	Today   StatsToday    `json:"today"`
	Session *StatsSession `json:"session,omitempty"`
	Limit   *StatsLimit   `json:"limit,omitempty"`
}

// StatsSync is the local sync state.
type StatsSync struct {
	State        string `json:"state"`
	BacklogBytes int64  `json:"backlog_bytes"`
	LastAck      string `json:"last_ack,omitempty"` // RFC 3339 UTC, omitted if never
}

// StatsToday is today's figures across devices.
type StatsToday struct {
	CostUSD    float64 `json:"cost_usd"`
	Tokens     int64   `json:"tokens"`
	Sessions   int64   `json:"sessions"`
	AgentTimeS int64   `json:"agent_time_s"`
	Failures   int64   `json:"failures"`
}

// StatsSession is the figures of the requested session.
type StatsSession struct {
	CostUSD  float64 `json:"cost_usd"`
	Tokens   int64   `json:"tokens"`
	Failures int64   `json:"failures"`
}

// StatsLimit is the active usage limit.
type StatsLimit struct {
	Type     string `json:"type"`
	ResetsAt string `json:"resets_at"` // RFC 3339 UTC
}

// statsCache is the on-disk cache; it never holds the token.
type statsCache struct {
	FetchedAt time.Time   `json:"fetched_at"`
	Server    string      `json:"server"`
	Session   string      `json:"session"`
	Self      client.Self `json:"self"`
}

func (e *env) now() time.Time {
	if e.h.Now != nil {
		return e.h.Now()
	}
	return time.Now()
}

func (e *env) readCache(server string) (statsCache, bool) {
	var c statsCache
	b, err := os.ReadFile(filepath.Join(e.dirs.Data, "stats-cache.json"))
	if err != nil || json.Unmarshal(b, &c) != nil || c.Server != server {
		return statsCache{}, false
	}
	return c, true
}

func (e *env) writeCache(c statsCache) {
	b, err := json.Marshal(c)
	if err != nil {
		return
	}
	f, err := os.CreateTemp(e.dirs.Data, "stats-cache-*.tmp") // CreateTemp makes it 0600
	if err != nil {
		return
	}
	_, werr := f.Write(b)
	if cerr := f.Close(); werr != nil || cerr != nil || os.Rename(f.Name(), filepath.Join(e.dirs.Data, "stats-cache.json")) != nil {
		_ = os.Remove(f.Name())
	}
}

func (e *env) stats(args []string) int {
	var session string
	switch {
	case len(args) == 1 && args[0] == "--json":
	case len(args) == 3 && args[0] == "--json" && args[1] == "--session" && args[2] != "":
		session = args[2]
	default:
		_, _ = fmt.Fprint(e.err, usage)
		return 2
	}
	db, err := e.openState()
	if err != nil {
		_, _ = fmt.Fprintln(e.err, "braid:", err)
		return 1
	}
	defer func() { _ = db.Close() }()
	server, _, _ := db.Setting(e.ctx, "server_url")
	kind, _, _ := db.Setting(e.ctx, "token_store")
	blocked, _, _ := db.Setting(e.ctx, "blocked")

	out := StatsOut{Version: StatsVersion, Sync: StatsSync{State: StateOK}}
	var self client.Self
	var haveSelf bool
	var token string
	if server != "" {
		token, _ = e.store(kind).Get(server) // any lookup error means no usable token
	}
	switch token {
	case "":
		out.Sync.State = StateLoggedOut
	default:
		now := e.now()
		c, ok := e.readCache(server)
		if ok && c.Session == session && c.FetchedAt.Before(now) && now.Sub(c.FetchedAt) < StatsTTL {
			self, haveSelf = c.Self, true
			break
		}
		ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
		defer cancel()
		fresh, ferr := client.New(server, token, Version, nil).StatsSelf(ctx, session)
		switch {
		case ferr == nil:
			self, haveSelf = fresh, true
			e.writeCache(statsCache{FetchedAt: now, Server: server, Session: session, Self: fresh})
		case errors.Is(ferr, client.ErrUnauthorized):
			out.Sync.State = StateLoggedOut
			_ = os.Remove(filepath.Join(e.dirs.Data, "stats-cache.json"))
		default:
			out.Sync.State = StateOffline
			if ok {
				self, haveSelf = c.Self, true
				if c.Session != session {
					self.Session = nil
				}
			}
		}
	}
	if haveSelf {
		t := self.Today
		out.Today = StatsToday{CostUSD: t.CostUSD, Tokens: t.Tokens, Sessions: t.SessionCount, AgentTimeS: t.AgentMs / 1000, Failures: t.Failures}
		if session != "" && self.Session != nil {
			out.Session = &StatsSession{CostUSD: self.Session.CostUSD, Tokens: self.Session.Tokens, Failures: self.Session.Failures}
		}
		if self.Limit != nil {
			out.Limit = &StatsLimit{Type: self.Limit.Type, ResetsAt: self.Limit.ResetsAt.UTC().Format(time.RFC3339)}
		}
	}
	if b, err := db.Backlog(e.ctx); err == nil {
		out.Sync.BacklogBytes = b.Bytes
		if !b.LastAck.IsZero() {
			out.Sync.LastAck = b.LastAck.UTC().Format(time.RFC3339)
		}
		// precedence: logged_out > blocked > offline > backlog > ok
		switch {
		case out.Sync.State == StateLoggedOut:
		case blocked != "":
			out.Sync.State = StateBlocked
		case out.Sync.State == StateOffline:
		case b.Files > 0:
			out.Sync.State = StateBacklog
		}
	} else {
		_, _ = fmt.Fprintln(e.err, "braid:", err)
		return 1
	}
	line, _ := json.Marshal(out)
	_, _ = fmt.Fprintf(e.out, "%s\n", line)
	return 0
}
