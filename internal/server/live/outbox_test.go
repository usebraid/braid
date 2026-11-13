// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package live

import "testing"

func TestOutboxCoalescesAndResyncsOnOverflow(t *testing.T) {
	ob := newOutbox(2)
	a := Message{Type: "session_updated", SessionID: "a"}
	ob.push(a)
	ob.push(a) // identical pending message is dropped
	if m, ok := ob.pop(); !ok || m != a {
		t.Fatalf("%v %v", m, ok)
	}
	if _, ok := ob.pop(); ok {
		t.Fatal("duplicate was queued")
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		ob.push(Message{Type: "session_updated", SessionID: id})
	}
	if m, ok := ob.pop(); !ok || m.Type != "resync" {
		t.Fatalf("overflow must yield resync: %v %v", m, ok)
	}
	if _, ok := ob.pop(); ok {
		t.Fatal("queue should be empty after resync")
	}
}
