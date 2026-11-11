-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 The Braid Authors
-- Schema, grants and row-level security. Runs as braid_owner; roles must already exist.

CREATE TABLE users (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), oidc_issuer text NOT NULL, oidc_sub text NOT NULL,
  email text, name text, is_admin boolean NOT NULL DEFAULT false,
  disabled_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(), last_login_at timestamptz,
  UNIQUE (oidc_issuer, oidc_sub));

CREATE TABLE web_sessions (
  id_hash bytea PRIMARY KEY, user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(), last_seen_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL);

CREATE TABLE devices (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
  name text NOT NULL, hostname text, os text, arch text, os_user text, daemon_version text,
  created_at timestamptz NOT NULL DEFAULT now(), last_seen_at timestamptz, last_sync_at timestamptz, revoked_at timestamptz,
  UNIQUE (id, user_id)); -- target of the composite FKs below: a child row can never point at another user's parent

CREATE TABLE device_codes (
  device_code_hash bytea PRIMARY KEY, user_code_hash bytea NOT NULL UNIQUE,
  client_info jsonb NOT NULL, ip inet,
  status text NOT NULL CHECK (status IN ('pending','approved','denied','redeemed')),
  user_id uuid REFERENCES users ON DELETE CASCADE, device_id uuid REFERENCES devices ON DELETE CASCADE,
  last_poll_at timestamptz, interval_s int NOT NULL DEFAULT 5,
  created_at timestamptz NOT NULL DEFAULT now(), expires_at timestamptz NOT NULL);

CREATE TABLE device_tokens (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  device_id uuid NOT NULL, user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
  hash bytea NOT NULL UNIQUE, scopes text[] NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(), last_used_at timestamptz, revoked_at timestamptz,
  FOREIGN KEY (device_id, user_id) REFERENCES devices (id, user_id) ON DELETE CASCADE);

CREATE TABLE source_files (
  id bigserial PRIMARY KEY, user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
  device_id uuid NOT NULL,
  rel_path text NOT NULL, head_hash text NOT NULL, kind text NOT NULL,
  end_offset bigint NOT NULL DEFAULT 0,
  first_seen timestamptz NOT NULL, last_seen timestamptz NOT NULL,
  UNIQUE (user_id, device_id, rel_path, head_hash),
  UNIQUE (id, user_id),
  FOREIGN KEY (device_id, user_id) REFERENCES devices (id, user_id) ON DELETE CASCADE);

CREATE TABLE raw_lines (
  id bigserial PRIMARY KEY, user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
  source_file_id bigint NOT NULL,
  "offset" bigint NOT NULL, line_hash bytea NOT NULL, line bytea NOT NULL, received_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, source_file_id, "offset", line_hash),
  FOREIGN KEY (source_file_id, user_id) REFERENCES source_files (id, user_id) ON DELETE CASCADE);
CREATE INDEX raw_lines_source_offset ON raw_lines (source_file_id, "offset");

CREATE TABLE raw_snapshots (
  id bigserial PRIMARY KEY, user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
  source_file_id bigint NOT NULL,
  content_hash bytea NOT NULL, content bytea NOT NULL, received_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, source_file_id, content_hash),
  FOREIGN KEY (source_file_id, user_id) REFERENCES source_files (id, user_id) ON DELETE CASCADE);

CREATE TABLE audit_log (
  id bigserial PRIMARY KEY, at timestamptz NOT NULL DEFAULT now(),
  actor_user_id uuid, subject_user_id uuid, device_id uuid,
  action text NOT NULL CHECK (action IN ('login','login_denied','logout','device_code_requested','device_approved',
    'device_denied','device_code_failed','device_renamed','device_revoked','token_expired','user_disabled',
    'user_enabled','user_deleted')),
  ip inet, user_agent text, detail jsonb);
CREATE INDEX audit_log_subject ON audit_log (subject_user_id, id DESC);
CREATE INDEX audit_log_id ON audit_log (id DESC);

-- Grants
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM PUBLIC;
REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM PUBLIC;
ALTER DEFAULT PRIVILEGES REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO braid_app;
GRANT SELECT, UPDATE (name, email) ON users TO braid_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON web_sessions TO braid_app;
GRANT SELECT, UPDATE (name, last_seen_at, last_sync_at, revoked_at) ON devices TO braid_app;
GRANT SELECT, UPDATE (revoked_at) ON device_tokens TO braid_app;
GRANT SELECT, INSERT, UPDATE (end_offset, last_seen) ON source_files TO braid_app;
GRANT SELECT, INSERT ON raw_lines, raw_snapshots TO braid_app;
GRANT SELECT ON audit_log TO braid_app;
GRANT USAGE ON SEQUENCE source_files_id_seq, raw_lines_id_seq, raw_snapshots_id_seq TO braid_app;

-- Row-level security. braid_owner has BYPASSRLS, so FORCE only constrains other roles.
CREATE FUNCTION app_user() RETURNS uuid LANGUAGE sql STABLE
  AS $$ SELECT nullif(current_setting('app.user_id', true), '')::uuid $$;
REVOKE EXECUTE ON FUNCTION app_user() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION app_user() TO braid_app;

DO $$
DECLARE t text; col text;
BEGIN
  FOREACH t IN ARRAY ARRAY['users','web_sessions','devices','device_tokens','source_files','raw_lines','raw_snapshots','audit_log'] LOOP
    col := CASE t WHEN 'users' THEN 'id' WHEN 'audit_log' THEN 'subject_user_id' ELSE 'user_id' END;
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY %I ON %I FOR ALL USING (%I = app_user()) WITH CHECK (%I = app_user())', t || '_isolation', t, col, col);
  END LOOP;
END $$;
