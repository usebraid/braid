#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 The Braid Authors
# Creates the Braid roles and database on first container start (mount into
# /docker-entrypoint-initdb.d). Needs BRAID_DB_OWNER_PASSWORD and BRAID_DB_APP_PASSWORD.
set -euo pipefail
: "${BRAID_DB_OWNER_PASSWORD:?}" "${BRAID_DB_APP_PASSWORD:?}"
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres <<'SQL'
\getenv owner_pw BRAID_DB_OWNER_PASSWORD
\getenv app_pw BRAID_DB_APP_PASSWORD
-- braid_owner needs BYPASSRLS: tables use FORCE ROW LEVEL SECURITY and the definer functions run as owner.
CREATE ROLE braid_owner LOGIN BYPASSRLS PASSWORD :'owner_pw';
CREATE ROLE braid_app LOGIN NOBYPASSRLS PASSWORD :'app_pw';
CREATE DATABASE braid OWNER braid_owner;
REVOKE ALL ON DATABASE braid FROM PUBLIC;
GRANT CONNECT ON DATABASE braid TO braid_app;
SQL
