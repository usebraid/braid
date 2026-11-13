-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 The Braid Authors

-- Agent time is the active time of an agent's own transcript (spec 02 section 4). The running state of the turn in
-- progress lives on the agent row so that incremental derivation equals a reindex at any batch border.
-- turn_count now counts turns with at least one assistant line; turn_ms, est_ms and result_ms stay but are no longer
-- the source of agent time. Existing rows keep active_ms = 0 until the parser-version bump rebuilds them.
-- agents keeps its RLS policy and grants (new columns need none).
ALTER TABLE agents
  DROP COLUMN duration_ms,
  DROP COLUMN duration_estimated,
  ADD COLUMN active_ms bigint NOT NULL DEFAULT 0,
  ADD COLUMN work_prev timestamptz,                          -- last point of the open turn; NULL: no turn started yet
  ADD COLUMN pending_ms bigint NOT NULL DEFAULT 0,           -- time of the open turn before its first assistant line
  ADD COLUMN turn_asst boolean NOT NULL DEFAULT false,       -- the open turn has produced an assistant line
  ADD COLUMN duration_ms bigint GENERATED ALWAYS AS (active_ms) STORED,
  ADD COLUMN duration_estimated boolean GENERATED ALWAYS AS (started_at IS NULL) STORED; -- no usable timestamps at all
