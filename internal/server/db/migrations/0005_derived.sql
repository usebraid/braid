-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 The Braid Authors
-- Derived tables (02 section 3): a rebuildable cache over raw_*; app may write and delete them.

CREATE EXTENSION IF NOT EXISTS pg_trgm;
ALTER TABLE users ADD COLUMN time_zone text;                    -- NULL = unset, effective 'UTC'
GRANT UPDATE (time_zone) ON users TO braid_app;
CREATE INDEX raw_lines_source_id ON raw_lines (source_file_id, id);

CREATE TABLE derive_state (
  user_id uuid PRIMARY KEY REFERENCES users ON DELETE CASCADE,
  parser_version int NOT NULL, rebuilding boolean NOT NULL DEFAULT false, updated_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE derive_cursor (
  user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE, source_file_id bigint NOT NULL,
  kind text NOT NULL CHECK (kind IN ('main','subagent','agent_meta','custom_title','other')),
  session_id text, agent_id text,                                 -- from the path; agent 'main' for the main transcript
  last_line_id bigint NOT NULL DEFAULT 0, last_snapshot_id bigint NOT NULL DEFAULT 0, last_ts timestamptz,
  PRIMARY KEY (user_id, source_file_id),
  FOREIGN KEY (source_file_id, user_id) REFERENCES source_files (id, user_id) ON DELETE CASCADE);
CREATE INDEX derive_cursor_agent ON derive_cursor (user_id, session_id, agent_id);
CREATE TABLE derive_errors (
  user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE, source_file_id bigint NOT NULL,
  errors bigint NOT NULL DEFAULT 0, last_offset bigint,
  PRIMARY KEY (user_id, source_file_id),
  FOREIGN KEY (source_file_id, user_id) REFERENCES source_files (id, user_id) ON DELETE CASCADE);

CREATE TABLE projects (user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE, path text NOT NULL, name text NOT NULL,
  first_seen_at timestamptz NOT NULL, last_seen_at timestamptz NOT NULL, PRIMARY KEY (user_id, path));

CREATE TABLE sessions (
  user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE, session_id text NOT NULL,
  project_path text, project_at timestamptz,                      -- cwd of the earliest main-agent line
  primary_device_id uuid,                                         -- device with most lines (session_devices)
  custom_title text, ai_title text, first_prompt text, first_prompt_at timestamptz,   -- first_prompt <= 80 chars
  title text GENERATED ALWAYS AS (coalesce(custom_title, ai_title, first_prompt)) STORED,
  git_branch text, git_branch_at timestamptz,                     -- from the latest line
  started_at timestamptz, ended_at timestamptz,
  models text[] NOT NULL DEFAULT '{}', line_count bigint NOT NULL DEFAULT 0,
  PRIMARY KEY (user_id, session_id));
CREATE INDEX sessions_started ON sessions (user_id, started_at DESC, session_id);
CREATE TABLE session_devices (
  user_id uuid NOT NULL, session_id text NOT NULL, device_id uuid NOT NULL, line_count bigint NOT NULL DEFAULT 0,
  PRIMARY KEY (user_id, session_id, device_id),
  FOREIGN KEY (device_id, user_id) REFERENCES devices (id, user_id) ON DELETE CASCADE);

CREATE TABLE agents (
  user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE, session_id text NOT NULL, agent_id text NOT NULL,
  agent_type text, description text, parent_agent_id text, parent_hint text,   -- parent_hint = meta.parentAgentId
  spawn_tool_use_id text, spawn_depth int NOT NULL DEFAULT 0, finished boolean,
  started_at timestamptz, ended_at timestamptz,
  turn_ms bigint NOT NULL DEFAULT 0, turn_count int NOT NULL DEFAULT 0,       -- sum / count of turn_duration lines
  result_ms bigint,                                                           -- Agent tool result totalDurationMs or task-notification duration
  est_ms bigint NOT NULL DEFAULT 0,                                           -- capped-gap estimate
  duration_ms bigint GENERATED ALWAYS AS (coalesce(result_ms, nullif(turn_ms, 0), est_ms)) STORED,
  duration_estimated boolean GENERATED ALWAYS AS (result_ms IS NULL AND turn_ms = 0) STORED,
  PRIMARY KEY (user_id, session_id, agent_id));

CREATE TABLE tool_calls (
  user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE, session_id text NOT NULL, tool_use_id text NOT NULL,
  agent_id text NOT NULL, tool_name text NOT NULL DEFAULT '', skill text,
  started_at timestamptz, ended_at timestamptz, is_error boolean NOT NULL DEFAULT false,
  result_ms bigint,                 -- toolUseResult.durationMs
  agent_total_ms bigint,            -- Agent tool result totalDurationMs or task-notification duration
  input_summary text NOT NULL DEFAULT '',                                     -- <= 200 chars
  duration_ms bigint GENERATED ALWAYS AS (coalesce(result_ms, (extract(epoch FROM (ended_at - started_at)) * 1000)::bigint)) STORED,
  PRIMARY KEY (user_id, session_id, tool_use_id));
CREATE INDEX tool_calls_agent ON tool_calls (user_id, session_id, agent_id);

CREATE TABLE skill_phases (
  user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE, session_id text NOT NULL, agent_id text NOT NULL, seq int NOT NULL,
  skill text NOT NULL, tool_use_id text NOT NULL, started_at timestamptz, ended_at timestamptz,
  PRIMARY KEY (user_id, session_id, agent_id, seq));

CREATE TABLE usage (
  user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE, message_id text NOT NULL,
  session_id text NOT NULL, agent_id text NOT NULL, model text NOT NULL,
  input_tokens bigint NOT NULL, output_tokens bigint NOT NULL, cache_read_tokens bigint NOT NULL,
  cache_write_5m_tokens bigint NOT NULL, cache_write_1h_tokens bigint NOT NULL,
  thinking_tokens bigint NOT NULL,                                            -- subset of output_tokens, never added again
  web_search_requests int NOT NULL DEFAULT 0, web_fetch_requests int NOT NULL DEFAULT 0, speed text NOT NULL DEFAULT '',
  at timestamptz NOT NULL, source_file_id bigint NOT NULL, "offset" bigint NOT NULL,
  cost_usd numeric(14,6) NOT NULL DEFAULT 0, price_unknown boolean NOT NULL DEFAULT false,
  PRIMARY KEY (user_id, message_id));
CREATE INDEX usage_at ON usage (user_id, at);
CREATE INDEX usage_session ON usage (user_id, session_id);

CREATE TABLE failures (
  user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE, source_file_id bigint NOT NULL, "offset" bigint NOT NULL,
  category text NOT NULL CHECK (category IN ('usage_limit','rate_limit','server_error','network','refusal','tool_error','other_api_error')),
  detail text NOT NULL DEFAULT '', occurred_at timestamptz NOT NULL, session_id text NOT NULL, agent_id text NOT NULL,
  limit_type text, resets_at timestamptz,
  PRIMARY KEY (user_id, source_file_id, "offset"));
CREATE INDEX failures_at ON failures (user_id, occurred_at DESC);

CREATE TABLE search_docs (
  user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE, source_file_id bigint NOT NULL, "offset" bigint NOT NULL,
  session_id text NOT NULL, agent_id text NOT NULL, role text NOT NULL CHECK (role IN ('user','assistant')), at timestamptz,
  tsv tsvector NOT NULL,            -- 'simple' configuration; the text itself is never stored
  idents text NOT NULL DEFAULT '',  -- tool names and file paths only, for partial matches
  PRIMARY KEY (user_id, source_file_id, "offset"));
CREATE INDEX search_docs_tsv ON search_docs USING gin (tsv);
CREATE INDEX search_docs_idents ON search_docs USING gin (idents gin_trgm_ops);
CREATE INDEX search_docs_session ON search_docs (user_id, session_id);

GRANT SELECT, INSERT, UPDATE, DELETE ON derive_state, derive_cursor, derive_errors, projects, sessions, session_devices, agents, tool_calls, skill_phases, usage, failures, search_docs TO braid_app;

DO $$
DECLARE t text; f regprocedure;
BEGIN
  FOREACH t IN ARRAY ARRAY['derive_state', 'derive_cursor', 'derive_errors', 'projects', 'sessions', 'session_devices', 'agents', 'tool_calls', 'skill_phases', 'usage', 'failures', 'search_docs'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY %I ON %I FOR ALL USING (user_id = app_user()) WITH CHECK (user_id = app_user())', t || '_isolation', t);
  END LOOP;
  -- 0001 revoked default function privileges from PUBLIC
  FOR f IN SELECT d.objid::regprocedure FROM pg_depend d JOIN pg_extension e ON e.oid = d.refobjid
            WHERE d.refclassid = 'pg_extension'::regclass AND d.classid = 'pg_proc'::regclass AND d.deptype = 'e' AND e.extname = 'pg_trgm' LOOP
    EXECUTE format('GRANT EXECUTE ON FUNCTION %s TO braid_app', f);
  END LOOP;
END $$;

-- Worker functions: unscoped sessions only.
CREATE FUNCTION derive_all_users() RETURNS SETOF uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $f$
BEGIN
  IF app_user() IS NOT NULL THEN RAISE EXCEPTION 'not allowed in a user-scoped session' USING ERRCODE = '42501'; END IF;
  RETURN QUERY SELECT id FROM users WHERE disabled_at IS NULL;
END $f$;

CREATE FUNCTION derive_pending_users() RETURNS SETOF uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp AS $f$
BEGIN
  IF app_user() IS NOT NULL THEN RAISE EXCEPTION 'not allowed in a user-scoped session' USING ERRCODE = '42501'; END IF;
  RETURN QUERY SELECT DISTINCT f.user_id FROM source_files f
    LEFT JOIN derive_cursor c ON c.source_file_id = f.id AND c.user_id = f.user_id
   WHERE (f.kind = 'append' AND EXISTS (SELECT 1 FROM raw_lines l WHERE l.source_file_id = f.id AND l.id > coalesce(c.last_line_id, 0)))
      OR (f.kind = 'snapshot' AND (SELECT max(s.id) FROM raw_snapshots s WHERE s.source_file_id = f.id) > coalesce(c.last_snapshot_id, 0));
END $f$;

REVOKE EXECUTE ON FUNCTION derive_all_users(), derive_pending_users() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION derive_all_users(), derive_pending_users() TO braid_app;
