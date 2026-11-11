-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 The Braid Authors
-- SECURITY DEFINER functions (owned by braid_owner, which has BYPASSRLS).

CREATE FUNCTION auth_session(p_id_hash bytea) RETURNS TABLE(user_id uuid, is_admin boolean)
LANGUAGE sql SECURITY DEFINER SET search_path = public, pg_temp AS $$
  UPDATE web_sessions s
     SET last_seen_at = now(), expires_at = least(s.created_at + interval '30 days', now() + interval '7 days')
    FROM users u
   WHERE s.id_hash = p_id_hash AND s.expires_at > now() AND u.id = s.user_id AND u.disabled_at IS NULL
  RETURNING s.user_id, u.is_admin
$$;

CREATE FUNCTION auth_device_token(p_hash bytea)
RETURNS TABLE(token_id uuid, device_id uuid, user_id uuid, scopes text[])
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
#variable_conflict use_column
DECLARE t device_tokens%ROWTYPE;
BEGIN
  SELECT dt.* INTO t
    FROM device_tokens dt JOIN devices d ON d.id = dt.device_id JOIN users u ON u.id = dt.user_id
   WHERE dt.hash = p_hash AND dt.revoked_at IS NULL AND d.revoked_at IS NULL AND u.disabled_at IS NULL
     FOR UPDATE OF dt;
  IF NOT FOUND THEN RETURN; END IF;
  IF coalesce(t.last_used_at, t.created_at) <= now() - interval '90 days' THEN
    UPDATE device_tokens SET revoked_at = now() WHERE id = t.id;
    INSERT INTO audit_log(subject_user_id, device_id, action) VALUES (t.user_id, t.device_id, 'token_expired');
    RETURN;
  END IF;
  UPDATE device_tokens SET last_used_at = now() WHERE id = t.id;
  UPDATE devices SET last_seen_at = now() WHERE id = t.device_id;
  RETURN QUERY SELECT t.id, t.device_id, t.user_id, t.scopes;
END $$;

CREATE FUNCTION upsert_oidc_user(p_issuer text, p_sub text, p_email text, p_name text, p_is_admin boolean)
RETURNS TABLE(user_id uuid, disabled boolean)
LANGUAGE sql SECURITY DEFINER SET search_path = public, pg_temp AS $$
  INSERT INTO users(oidc_issuer, oidc_sub, email, name, is_admin, last_login_at)
  VALUES (p_issuer, p_sub, p_email, p_name, p_is_admin, now())
  ON CONFLICT (oidc_issuer, oidc_sub)
  DO UPDATE SET email = excluded.email, name = excluded.name, is_admin = excluded.is_admin, last_login_at = now()
  RETURNING id, disabled_at IS NOT NULL
$$;

CREATE FUNCTION device_code_create(p_device_code_hash bytea, p_user_code_hash bytea, p_client_info jsonb, p_ip inet)
RETURNS void LANGUAGE sql SECURITY DEFINER SET search_path = public, pg_temp AS $$
  DELETE FROM device_codes WHERE expires_at < now() - interval '1 day';
  INSERT INTO device_codes(device_code_hash, user_code_hash, client_info, ip, status, created_at, expires_at, interval_s)
  VALUES (p_device_code_hash, p_user_code_hash, p_client_info, p_ip, 'pending', now(), now() + interval '10 minutes', 5);
$$;

CREATE FUNCTION device_code_lookup(p_user_code_hash bytea)
RETURNS TABLE(client_info jsonb, ip inet, created_at timestamptz)
LANGUAGE sql SECURITY DEFINER SET search_path = public, pg_temp AS $$
  SELECT c.client_info, c.ip, c.created_at FROM device_codes c
   WHERE c.user_code_hash = p_user_code_hash AND c.status = 'pending' AND c.expires_at > now()
$$;

-- Returns the new device id on approve, the nil UUID on a successful deny, NULL when nothing matched.
CREATE FUNCTION device_code_decide(p_user_code_hash bytea, p_approve boolean, p_device_name text) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
DECLARE
  v_user uuid := app_user();
  v_name text := btrim(p_device_name);
  c device_codes%ROWTYPE;
  v_dev uuid;
BEGIN
  IF v_user IS NULL OR NOT EXISTS (SELECT 1 FROM users WHERE id = v_user AND disabled_at IS NULL) THEN RETURN NULL; END IF;
  IF p_approve AND (v_name IS NULL OR v_name = '' OR char_length(v_name) > 64) THEN RETURN NULL; END IF;
  SELECT * INTO c FROM device_codes
   WHERE user_code_hash = p_user_code_hash AND status = 'pending' AND expires_at > now() FOR UPDATE;
  IF NOT FOUND THEN RETURN NULL; END IF;
  IF p_approve THEN
    INSERT INTO devices(user_id, name, hostname, os, arch, os_user, daemon_version, created_at)
    VALUES (v_user, v_name, c.client_info->>'hostname', c.client_info->>'os', c.client_info->>'arch',
            c.client_info->>'os_user', c.client_info->>'daemon_version', now())
    RETURNING id INTO v_dev;
    UPDATE device_codes SET status = 'approved', user_id = v_user, device_id = v_dev WHERE device_code_hash = c.device_code_hash;
    INSERT INTO audit_log(actor_user_id, subject_user_id, device_id, action, ip, detail)
    VALUES (v_user, v_user, v_dev, 'device_approved', c.ip, c.client_info);
    RETURN v_dev;
  END IF;
  UPDATE device_codes SET status = 'denied', user_id = v_user WHERE device_code_hash = c.device_code_hash;
  INSERT INTO audit_log(actor_user_id, subject_user_id, action, ip, detail)
  VALUES (v_user, v_user, 'device_denied', c.ip, c.client_info);
  RETURN '00000000-0000-0000-0000-000000000000'::uuid;
END $$;

CREATE FUNCTION device_code_poll(p_device_code_hash bytea, p_token_hash bytea)
RETURNS TABLE(status text, device_id uuid, interval_s int)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
#variable_conflict use_column
DECLARE c device_codes%ROWTYPE;
BEGIN
  SELECT * INTO c FROM device_codes WHERE device_code_hash = p_device_code_hash FOR UPDATE;
  IF NOT FOUND THEN RETURN QUERY SELECT 'invalid_grant', NULL::uuid, 0; RETURN; END IF;
  IF c.expires_at <= now() THEN RETURN QUERY SELECT 'expired_token', NULL::uuid, c.interval_s; RETURN; END IF;
  IF c.last_poll_at IS NOT NULL AND c.last_poll_at + c.interval_s * interval '1 second' > now() THEN
    UPDATE device_codes SET interval_s = interval_s + 5, last_poll_at = now() WHERE device_code_hash = c.device_code_hash;
    RETURN QUERY SELECT 'slow_down', NULL::uuid, c.interval_s + 5; RETURN;
  END IF;
  UPDATE device_codes SET last_poll_at = now() WHERE device_code_hash = c.device_code_hash;
  IF c.status = 'pending' THEN RETURN QUERY SELECT 'authorization_pending', NULL::uuid, c.interval_s; RETURN; END IF;
  IF c.status = 'denied' THEN RETURN QUERY SELECT 'access_denied', NULL::uuid, c.interval_s; RETURN; END IF;
  IF c.status <> 'approved' THEN RETURN QUERY SELECT 'invalid_grant', NULL::uuid, c.interval_s; RETURN; END IF;
  IF NOT EXISTS (SELECT 1 FROM users WHERE id = c.user_id AND disabled_at IS NULL) THEN
    RETURN QUERY SELECT 'access_denied', NULL::uuid, c.interval_s; RETURN;
  END IF;
  INSERT INTO device_tokens(device_id, user_id, hash, scopes, created_at)
  VALUES (c.device_id, c.user_id, p_token_hash, ARRAY['ingest:write','stats:read:self'], now());
  UPDATE device_codes SET status = 'redeemed' WHERE device_code_hash = c.device_code_hash;
  RETURN QUERY SELECT 'ok', c.device_id, c.interval_s;
END $$;

CREATE FUNCTION device_token_revoke(p_hash bytea) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
DECLARE v_dev uuid; v_user uuid;
BEGIN
  UPDATE device_tokens SET revoked_at = coalesce(revoked_at, now()) WHERE hash = p_hash
  RETURNING device_id, user_id INTO v_dev, v_user;
  IF NOT FOUND THEN RETURN false; END IF;
  UPDATE devices SET revoked_at = coalesce(revoked_at, now()) WHERE id = v_dev;
  INSERT INTO audit_log(actor_user_id, subject_user_id, device_id, action) VALUES (v_user, v_user, v_dev, 'device_revoked');
  RETURN true;
END $$;

CREATE FUNCTION write_audit(p_actor uuid, p_subject uuid, p_device uuid, p_action text, p_ip inet, p_ua text, p_detail jsonb)
RETURNS void LANGUAGE sql SECURITY DEFINER SET search_path = public, pg_temp AS $$
  INSERT INTO audit_log(actor_user_id, subject_user_id, device_id, action, ip, user_agent, detail)
  VALUES (p_actor, p_subject, p_device, p_action, p_ip, p_ua, p_detail)
$$;

-- Internal guard, not granted to braid_app.
CREATE FUNCTION require_admin() RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
DECLARE v_user uuid := app_user();
BEGIN
  IF v_user IS NULL OR NOT EXISTS (SELECT 1 FROM users WHERE id = v_user AND is_admin AND disabled_at IS NULL) THEN
    RAISE EXCEPTION 'admin required' USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN v_user;
END $$;

CREATE FUNCTION admin_list_users()
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
           + (SELECT coalesce(sum(octet_length(s.content)), 0) FROM raw_snapshots s WHERE s.user_id = u.id)::bigint,
         (SELECT max(d.last_sync_at) FROM devices d WHERE d.user_id = u.id)
    FROM users u ORDER BY u.created_at;
END $$;

CREATE FUNCTION admin_set_disabled(p_user uuid, p_disabled boolean) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
DECLARE v_admin uuid := require_admin();
BEGIN
  IF p_user = v_admin THEN RAISE EXCEPTION 'cannot change own disabled state'; END IF;
  IF p_disabled THEN
    UPDATE users SET disabled_at = coalesce(disabled_at, now()) WHERE id = p_user;
  ELSE
    UPDATE users SET disabled_at = NULL WHERE id = p_user;
  END IF;
  IF NOT FOUND THEN RAISE EXCEPTION 'user not found' USING ERRCODE = 'no_data_found'; END IF;
  IF p_disabled THEN
    DELETE FROM web_sessions WHERE user_id = p_user;
    UPDATE device_tokens SET revoked_at = now() WHERE user_id = p_user AND revoked_at IS NULL;
  END IF;
  INSERT INTO audit_log(actor_user_id, subject_user_id, action)
  VALUES (v_admin, p_user, CASE WHEN p_disabled THEN 'user_disabled' ELSE 'user_enabled' END);
END $$;

CREATE FUNCTION admin_delete_user(p_user uuid) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
DECLARE v_admin uuid := require_admin(); v_email text;
BEGIN
  IF p_user = v_admin THEN RAISE EXCEPTION 'cannot delete self'; END IF;
  SELECT email INTO v_email FROM users WHERE id = p_user;
  IF NOT FOUND THEN RAISE EXCEPTION 'user not found' USING ERRCODE = 'no_data_found'; END IF;
  INSERT INTO audit_log(actor_user_id, subject_user_id, action, detail)
  VALUES (v_admin, NULL, 'user_deleted', jsonb_build_object('user_id', p_user, 'email', v_email));
  DELETE FROM users WHERE id = p_user;
END $$;

CREATE FUNCTION admin_audit(p_before bigint, p_limit int) RETURNS SETOF audit_log
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $$
BEGIN
  PERFORM require_admin();
  RETURN QUERY SELECT * FROM audit_log a WHERE p_before IS NULL OR a.id < p_before
    ORDER BY a.id DESC LIMIT greatest(1, least(coalesce(p_limit, 50), 200));
END $$;

DO $$
DECLARE f regprocedure;
BEGIN
  FOR f IN SELECT p.oid::regprocedure FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
            WHERE n.nspname = 'public' AND p.proname <> 'app_user' LOOP
    EXECUTE format('REVOKE EXECUTE ON FUNCTION %s FROM PUBLIC', f);
    IF f::text NOT LIKE 'require_admin%' THEN EXECUTE format('GRANT EXECUTE ON FUNCTION %s TO braid_app', f); END IF;
  END LOOP;
END $$;
