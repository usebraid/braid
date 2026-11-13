// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package ingest

// HoldSlots occupies n concurrency slots and returns a release func (test hook).
func (h *Handler) HoldSlots(n int) func() {
	for i := 0; i < n; i++ {
		h.sem <- struct{}{}
	}
	return func() {
		for i := 0; i < n; i++ {
			<-h.sem
		}
	}
}

// AssemblySQL lists the statements that touch the chunks of a line (test hook for the query-shape check).
var AssemblySQL = []string{sqlPartsCheck, sqlPartsLine, sqlPartsSnapshot}
