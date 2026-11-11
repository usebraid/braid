// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package httpx holds shared HTTP building blocks for the server.
package httpx

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

func inPrefixes(a netip.Addr, ps []netip.Prefix) bool {
	for _, p := range ps {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// ClientIP returns the peer address, or, when the peer is a trusted proxy, the
// rightmost untrusted X-Forwarded-For hop (leftmost if all are trusted).
// A malformed hop ends the walk and falls back to the peer address.
func ClientIP(r *http.Request, trusted []netip.Prefix) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	remote, _ := netip.ParseAddr(host)
	remote = remote.Unmap()
	xff := strings.Join(r.Header.Values("X-Forwarded-For"), ",")
	if xff == "" || !inPrefixes(remote, trusted) {
		return remote
	}
	hops := strings.Split(xff, ",")
	var hop netip.Addr
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			return remote
		}
		hop = a.Unmap()
		if !inPrefixes(hop, trusted) {
			return hop
		}
	}
	return hop // all trusted: leftmost hop
}
