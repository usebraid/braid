-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 The Braid Authors

-- A background agent's tool result is only the launch ack; it returns with its <task-notification> (spec 06 section 5).
ALTER TABLE tool_calls ADD COLUMN background boolean NOT NULL DEFAULT false, ADD COLUMN note_at timestamptz;
