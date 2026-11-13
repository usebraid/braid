-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 The Braid Authors

-- Read-only twin of auth_session: validates a web session without sliding its expiry
-- (the live WebSocket re-checks periodically and must not keep an idle session alive).
CREATE FUNCTION auth_session_peek(p_id_hash bytea) RETURNS TABLE(user_id uuid, is_admin boolean)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public, pg_temp AS $$
  SELECT s.user_id, u.is_admin
    FROM web_sessions s JOIN users u ON u.id = s.user_id
   WHERE s.id_hash = p_id_hash AND s.expires_at > now() AND u.disabled_at IS NULL
$$;

REVOKE EXECUTE ON FUNCTION auth_session_peek(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION auth_session_peek(bytea) TO braid_app;
