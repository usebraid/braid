-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 The Braid Authors

-- The OIDC picture claim, stored at login. The server fetches it for /api/v1/me/avatar only from the issuer's origin.
-- users keeps its RLS policy (id = app_user()) and braid_app's table-wide SELECT; nothing grants UPDATE on the column,
-- only the login function below writes it.
ALTER TABLE users ADD COLUMN picture_url text;

DROP FUNCTION upsert_oidc_user(text, text, text, text, boolean);

-- Login runs unscoped; a scoped session calling this could make itself admin.
CREATE FUNCTION upsert_oidc_user(p_issuer text, p_sub text, p_email text, p_name text, p_is_admin boolean, p_picture text DEFAULT NULL)
RETURNS TABLE(user_id uuid, disabled boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
BEGIN
  IF app_user() IS NOT NULL THEN RAISE EXCEPTION 'not allowed in a user-scoped session' USING ERRCODE = '42501'; END IF;
  RETURN QUERY
  INSERT INTO users(oidc_issuer, oidc_sub, email, name, is_admin, picture_url, last_login_at)
  VALUES (p_issuer, p_sub, p_email, p_name, p_is_admin, p_picture, now())
  ON CONFLICT (oidc_issuer, oidc_sub)
  DO UPDATE SET email = excluded.email, name = excluded.name, is_admin = excluded.is_admin, picture_url = excluded.picture_url,
    last_login_at = now()
  RETURNING id, disabled_at IS NOT NULL;
END $$;
REVOKE EXECUTE ON FUNCTION upsert_oidc_user(text, text, text, text, boolean, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION upsert_oidc_user(text, text, text, text, boolean, text) TO braid_app;
