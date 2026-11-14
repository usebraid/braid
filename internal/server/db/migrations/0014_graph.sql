-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 The Braid Authors

-- Start and resume events of a session and the order of its lines (spec 06 section 5); rebuildable by reindex.
CREATE TABLE session_events (
  user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
  session_id text NOT NULL,
  kind text NOT NULL CHECK (kind IN ('start', 'resume')),
  at timestamptz NOT NULL,
  device_id uuid,            -- device of the line that is the event
  model text,                -- model of the first assistant line after the event (main agent); NULL until it exists
  source_file_id bigint NOT NULL, "offset" bigint NOT NULL,
  PRIMARY KEY (user_id, session_id, kind, source_file_id, "offset"));
CREATE INDEX session_events_at ON session_events (user_id, at DESC, session_id);

CREATE TABLE line_order (
  user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
  raw_line_id bigint NOT NULL,
  session_id text NOT NULL, agent_id text NOT NULL,
  ts timestamptz NOT NULL,   -- the line's timestamp, else the previous line's of the same agent, else the file's first timestamp ('-infinity' while the file has none)
  PRIMARY KEY (user_id, raw_line_id));
CREATE INDEX line_order_session ON line_order (user_id, session_id, ts, raw_line_id);

ALTER TABLE agents ADD COLUMN last_turn_device uuid;       -- device of the agent's last turn start, for the device-switch rule

GRANT SELECT, INSERT, UPDATE, DELETE ON session_events, line_order TO braid_app;

DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['session_events', 'line_order'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY %I ON %I FOR ALL USING (user_id = app_user()) WITH CHECK (user_id = app_user())', t || '_isolation', t);
  END LOOP;
END $$;
