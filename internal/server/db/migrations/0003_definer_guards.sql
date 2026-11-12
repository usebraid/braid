-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 The Braid Authors
-- Guards for the SECURITY DEFINER functions that bypass RLS: a request scoped to a user
-- (app.user_id set) must not be able to use them to act as someone else.

-- Login runs unscoped; a scoped session calling this could make itself admin.
CREATE OR REPLACE FUNCTION upsert_oidc_user(p_issuer text, p_sub text, p_email text, p_name text, p_is_admin boolean)
RETURNS TABLE(user_id uuid, disabled boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
BEGIN
  IF app_user() IS NOT NULL THEN RAISE EXCEPTION 'not allowed in a user-scoped session' USING ERRCODE = '42501'; END IF;
  RETURN QUERY
  INSERT INTO users(oidc_issuer, oidc_sub, email, name, is_admin, last_login_at)
  VALUES (p_issuer, p_sub, p_email, p_name, p_is_admin, now())
  ON CONFLICT (oidc_issuer, oidc_sub)
  DO UPDATE SET email = excluded.email, name = excluded.name, is_admin = excluded.is_admin, last_login_at = now()
  RETURNING id, disabled_at IS NOT NULL;
END $$;

-- A scoped session may only write audit entries about itself.
CREATE OR REPLACE FUNCTION write_audit(p_actor uuid, p_subject uuid, p_device uuid, p_action text, p_ip inet, p_ua text, p_detail jsonb)
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
BEGIN
  IF app_user() IS NOT NULL AND (p_actor IS DISTINCT FROM app_user() OR p_subject IS DISTINCT FROM app_user()) THEN
    RAISE EXCEPTION 'audit entry for another user' USING ERRCODE = '42501';
  END IF;
  INSERT INTO audit_log(actor_user_id, subject_user_id, device_id, action, ip, user_agent, detail)
  VALUES (p_actor, p_subject, p_device, p_action, p_ip, p_ua, p_detail);
END $$;
