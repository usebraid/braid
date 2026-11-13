-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 The Braid Authors

-- Removes chunks of lines that never completed; called by the derive sweep (runs across all users, so a definer function).
CREATE FUNCTION gc_line_parts() RETURNS bigint
LANGUAGE sql SECURITY DEFINER SET search_path = public, pg_temp AS $$
  WITH d AS (DELETE FROM raw_line_parts WHERE received_at < now() - interval '7 days' RETURNING 1) SELECT count(*) FROM d
$$;
REVOKE EXECUTE ON FUNCTION gc_line_parts() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION gc_line_parts() TO braid_app;
