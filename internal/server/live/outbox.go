// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package live

import "sync"

// outbox is a bounded per-connection queue. A message identical to a pending one is dropped; on overflow
// the queue is replaced by a single resync, which also swallows everything pushed until it is popped.
type outbox struct {
	mu   sync.Mutex
	q    []Message
	max  int
	wake chan struct{}
}

func newOutbox(size int) *outbox { return &outbox{max: size, wake: make(chan struct{}, 1)} }

func (o *outbox) push(m Message) {
	o.mu.Lock()
	switch {
	case len(o.q) > 0 && o.q[0].Type == "resync": // a resync is pending and covers everything else
	case m.Type == "resync":
		o.q = append(o.q[:0], m)
	case o.has(m):
	case len(o.q) >= o.max:
		o.q = append(o.q[:0], Message{Type: "resync"})
	default:
		o.q = append(o.q, m)
	}
	o.mu.Unlock()
	select {
	case o.wake <- struct{}{}:
	default:
	}
}

func (o *outbox) has(m Message) bool {
	for _, x := range o.q {
		if x == m {
			return true
		}
	}
	return false
}

func (o *outbox) pop() (Message, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.q) == 0 {
		return Message{}, false
	}
	m := o.q[0]
	o.q = o.q[1:]
	return m, true
}
