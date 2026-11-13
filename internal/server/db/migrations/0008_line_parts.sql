-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 The Braid Authors

-- Chunks of lines and snapshots too large for one ingest request. Scratch space: a line is
-- assembled into raw_lines/raw_snapshots when its last part arrives, then its parts are deleted;
-- parts older than 7 days are removed by the ingest handler.
CREATE TABLE raw_line_parts (
  user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
  source_file_id bigint NOT NULL,
  "offset" bigint NOT NULL,
  idx int NOT NULL,
  count int NOT NULL,
  total_len bigint NOT NULL,
  sha256 bytea NOT NULL,
  chunk bytea NOT NULL,
  received_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, source_file_id, "offset", idx),
  FOREIGN KEY (source_file_id, user_id) REFERENCES source_files (id, user_id) ON DELETE CASCADE);

GRANT SELECT, INSERT, UPDATE, DELETE ON raw_line_parts TO braid_app;
ALTER TABLE raw_line_parts ENABLE ROW LEVEL SECURITY;
ALTER TABLE raw_line_parts FORCE ROW LEVEL SECURITY;
CREATE POLICY raw_line_parts_isolation ON raw_line_parts FOR ALL USING (user_id = app_user()) WITH CHECK (user_id = app_user());

-- Stored bytes now include pending parts.
CREATE OR REPLACE FUNCTION admin_list_users()
RETURNS TABLE(id uuid, email text, name text, is_admin boolean, disabled_at timestamptz, last_login_at timestamptz,
              devices int, stored_bytes bigint, last_sync_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
#variable_conflict use_column
BEGIN
  PERFORM require_admin();
  RETURN QUERY
  SELECT u.id, u.email, u.name, u.is_admin, u.disabled_at, u.last_login_at,
         (SELECT count(*)::int FROM devices d WHERE d.user_id = u.id AND d.revoked_at IS NULL),
         (SELECT coalesce(sum(octet_length(l.line)), 0) FROM raw_lines l WHERE l.user_id = u.id)::bigint
           + (SELECT coalesce(sum(octet_length(s.content)), 0) FROM raw_snapshots s WHERE s.user_id = u.id)::bigint
           + (SELECT coalesce(sum(octet_length(p.chunk)), 0) FROM raw_line_parts p WHERE p.user_id = u.id)::bigint,
         (SELECT max(d.last_sync_at) FROM devices d WHERE d.user_id = u.id)
    FROM users u ORDER BY u.created_at;
END $$;
