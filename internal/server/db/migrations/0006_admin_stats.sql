-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 The Braid Authors

-- Per-user aggregates for the admin view. Numbers only, no text columns.
CREATE FUNCTION admin_stats()
RETURNS TABLE(user_id uuid, sessions bigint, cost_usd numeric, tokens bigint, last_activity_at timestamptz, derive_errors bigint, rebuilding boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
#variable_conflict use_column
BEGIN
  PERFORM require_admin();
  RETURN QUERY
  SELECT u.id,
         (SELECT count(*) FROM sessions s WHERE s.user_id = u.id),
         (SELECT coalesce(sum(x.cost_usd), 0) FROM usage x WHERE x.user_id = u.id),
         (SELECT coalesce(sum(x.input_tokens + x.output_tokens + x.cache_read_tokens + x.cache_write_5m_tokens + x.cache_write_1h_tokens), 0)::bigint
            FROM usage x WHERE x.user_id = u.id),
         greatest((SELECT max(x.at) FROM usage x WHERE x.user_id = u.id), (SELECT max(s.ended_at) FROM sessions s WHERE s.user_id = u.id)),
         (SELECT coalesce(sum(e.errors), 0)::bigint FROM derive_errors e WHERE e.user_id = u.id),
         coalesce((SELECT d.rebuilding FROM derive_state d WHERE d.user_id = u.id), false)
    FROM users u ORDER BY u.created_at;
END $$;

REVOKE EXECUTE ON FUNCTION admin_stats() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION admin_stats() TO braid_app;
