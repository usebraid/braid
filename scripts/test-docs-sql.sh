#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 The Braid Authors
# Runs the SQL block of site/docs/external-postgres.md on a fresh Postgres, then the real migrations as braid_owner,
# and checks the role attributes. Needs docker and go; psql runs inside the container.
set -euo pipefail
cd "$(dirname "$0")/.."
name=braid-docs-pg
image=$(sed -nE 's/^[[:space:]]*image:[[:space:]]*(postgres:16-alpine@sha256:[0-9a-f]+).*/\1/p' deploy/docker-compose.yml)
[ -n "$image" ] || { echo "no pinned postgres image in deploy/docker-compose.yml" >&2; exit 1; }
trap 'docker rm -f "$name" >/dev/null 2>&1 || true' EXIT
docker rm -f "$name" >/dev/null 2>&1 || true

sql=$(awk '/^<!-- sql:setup -->$/ {m=1; next} m && /^```sql$/ {f=1; next} f && /^```$/ {exit} f' site/docs/external-postgres.md)
[ -n "$sql" ] || { echo "no sql:setup block found" >&2; exit 1; }
owner_pw=$(openssl rand -hex 16) app_pw=$(openssl rand -hex 16)

docker run -d --name "$name" -e POSTGRES_PASSWORD=docs -p 127.0.0.1::5432 "$image" >/dev/null
port=$(docker port "$name" 5432/tcp | head -1 | sed 's/.*://')
until docker exec "$name" pg_isready -U postgres >/dev/null 2>&1; do sleep 1; done
sleep 2 # the entrypoint restarts the server once after initialisation
until docker exec "$name" pg_isready -U postgres >/dev/null 2>&1; do sleep 1; done

psql() { docker exec -i "$name" psql -X -v ON_ERROR_STOP=1 -At "$@"; }
printf '%s\n' "$sql" | psql -v owner_pw="$owner_pw" -v app_pw="$app_pw" -U postgres -d postgres

BRAID_DOCS_OWNER_URL="postgres://braid_owner:$owner_pw@127.0.0.1:$port/braid?sslmode=disable" \
  go test ./internal/server/db -run TestMigrateExternal -tags docs -count=1

attrs=$(psql -U postgres -d postgres -c "SELECT rolname||' '||rolsuper||' '||rolbypassrls FROM pg_roles WHERE rolname IN ('braid_owner','braid_app') ORDER BY 1")
[ "$attrs" = $'braid_app false false\nbraid_owner false true' ] || { echo "unexpected role attributes: $attrs" >&2; exit 1; }
[ "$(psql -U postgres -d braid -c "SELECT count(*) FROM pg_extension WHERE extname='pg_trgm'")" = 1 ] || { echo "pg_trgm missing" >&2; exit 1; }
# runtime role can connect and read what the migrations granted
PGPASSWORD=$app_pw docker exec -i -e PGPASSWORD "$name" psql -X -At -h 127.0.0.1 -U braid_app -d braid -c "SELECT 1 FROM schema_migrations LIMIT 0" >/dev/null 2>&1 \
  && { echo "braid_app can read schema_migrations (unexpected)" >&2; exit 1; }
PGPASSWORD=$app_pw docker exec -i -e PGPASSWORD "$name" psql -X -At -h 127.0.0.1 -U braid_app -d braid -c "SELECT count(*) FROM devices" >/dev/null
echo "docs sql ok"
