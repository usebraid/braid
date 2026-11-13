// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package live

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/usebraid/braid/internal/server/httpx"
)

// Message is one server-to-client text frame (JSON). It carries ids only, never data.
type Message struct {
	Type      string `json:"type"` // hello | ping | session_updated | device_synced | reindex | resync
	Protocol  int    `json:"protocol,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	DeviceID  string `json:"device_id,omitempty"`
	State     string `json:"state,omitempty"`
}

// ListenFunc is (*db.DB).Listen.
type ListenFunc func(ctx context.Context, channel string, ready func(), onNotify func(payload string)) error

// Options tunes the hub; zero values take the defaults.
type Options struct {
	MaxPerUser        int           // default 10; more -> 429
	MaxTotal          int           // default 500
	QueueSize         int           // default 32; overflow drops the queue and sends one resync
	PingEvery         time.Duration // default 25s
	RecheckEvery      time.Duration // default 1m: re-validate the cookie, close with 4401 if gone
	ConnectsPerMinute int           // default 30 per user
}

func (o *Options) defaults() {
	if o.MaxPerUser <= 0 {
		o.MaxPerUser = 10
	}
	if o.MaxTotal <= 0 {
		o.MaxTotal = 500
	}
	if o.QueueSize <= 0 {
		o.QueueSize = 32
	}
	if o.PingEvery <= 0 {
		o.PingEvery = 25 * time.Second
	}
	if o.RecheckEvery <= 0 {
		o.RecheckEvery = time.Minute
	}
	if o.ConnectsPerMinute <= 0 {
		o.ConnectsPerMinute = 30
	}
}

// Hub maps users to their connections and fans events out to the owner only.
type Hub struct {
	listen  ListenFunc
	o       Options
	limiter *httpx.Limiter

	mu    sync.Mutex
	users map[string]map[*outbox]struct{}
	total int

	ready     chan struct{}
	readyOnce sync.Once
	stop      chan struct{} // closed when Run returns: connections close with 1001
	stopOnce  sync.Once
}

// NewHub creates a hub; call Run to start listening.
func NewHub(listen ListenFunc, o Options) *Hub {
	o.defaults()
	return &Hub{listen: listen, o: o, limiter: httpx.NewLimiter(o.ConnectsPerMinute, time.Now),
		users: map[string]map[*outbox]struct{}{}, ready: make(chan struct{}), stop: make(chan struct{})}
}

// Ready is closed once both LISTENs are active.
func (h *Hub) Ready() <-chan struct{} { return h.ready }

// Run listens on both channels and fans events out until ctx is done.
func (h *Hub) Run(ctx context.Context) error {
	defer h.stopOnce.Do(func() { close(h.stop) })
	var mu sync.Mutex
	active := map[string]bool{} // channels that have been LISTENing at least once
	var wg sync.WaitGroup
	for _, ch := range []string{ChannelDerived, ChannelSynced} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = h.listen(ctx, ch, func() {
				mu.Lock()
				defer mu.Unlock()
				if active[ch] { // reconnected: events may have been missed
					h.resyncAll()
				}
				active[ch] = true
				if len(active) == 2 {
					h.readyOnce.Do(func() { close(h.ready) })
				}
			}, h.onNotify)
		}()
	}
	wg.Wait()
	return nil
}

func (h *Hub) onNotify(payload string) {
	var ev Event
	if json.Unmarshal([]byte(payload), &ev) != nil || ev.UserID == "" {
		slog.Warn("live: ignoring malformed event") // payload not logged
		return
	}
	var m Message
	switch ev.Type {
	case "session_updated":
		m = Message{Type: ev.Type, SessionID: ev.SessionID}
	case "device_synced":
		m = Message{Type: ev.Type, DeviceID: ev.DeviceID}
	case "reindex":
		m = Message{Type: ev.Type, State: ev.State}
	case "resync":
		m = Message{Type: ev.Type}
	default:
		return
	}
	h.Publish(ev.UserID, m)
}

// Publish queues m for every connection of userID, and for nobody else.
func (h *Hub) Publish(userID string, m Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ob := range h.users[userID] {
		ob.push(m)
	}
}

func (h *Hub) resyncAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, set := range h.users {
		for ob := range set {
			ob.push(Message{Type: "resync"})
		}
	}
}

// add registers a connection unless a cap is reached.
func (h *Hub) add(userID string) (*outbox, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.total >= h.o.MaxTotal || len(h.users[userID]) >= h.o.MaxPerUser {
		return nil, false
	}
	if h.users[userID] == nil {
		h.users[userID] = map[*outbox]struct{}{}
	}
	ob := newOutbox(h.o.QueueSize)
	h.users[userID][ob] = struct{}{}
	h.total++
	return ob, true
}

func (h *Hub) remove(userID string, ob *outbox) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.users[userID][ob]; !ok {
		return
	}
	delete(h.users[userID], ob)
	if len(h.users[userID]) == 0 {
		delete(h.users, userID)
	}
	h.total--
}
