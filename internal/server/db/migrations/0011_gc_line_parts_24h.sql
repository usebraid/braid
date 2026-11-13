-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 The Braid Authors

-- Unfinished chunk series free their quota after 24 hours without a new part (was 7 days per part).
-- A series is one (user, file, offset, device); it goes as a whole, so a slow upload never loses only its early parts.
CREATE OR REPLACE FUNCTION gc_line_parts() RETURNS bigint
LANGUAGE sql SECURITY DEFINER SET search_path = public, pg_temp AS $$
  WITH stale AS (
    SELECT user_id, source_file_id, "offset", device_id FROM raw_line_parts
     GROUP BY user_id, source_file_id, "offset", device_id HAVING max(received_at) < now() - interval '24 hours'),
  d AS (DELETE FROM raw_line_parts p USING stale s
         WHERE p.user_id = s.user_id AND p.source_file_id = s.source_file_id AND p."offset" = s."offset" AND p.device_id = s.device_id
         RETURNING 1)
  SELECT count(*) FROM d
$$;
