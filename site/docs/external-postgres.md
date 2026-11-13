---
title: External Postgres
order: 2
---

# External Postgres

The bundled Compose file starts its own Postgres. To use a database you already run, prepare it as below and drop the bundled service.

## Requirements

- PostgreSQL 16 or newer.
- The `pg_trgm` extension available.
- Two roles, `braid_owner` (runs the migrations) and `braid_app` (what the server uses at runtime), and a database `braid`.

## Create roles and database

Run this as a superuser (or an admin who may create roles and databases). Set the two passwords as psql variables, for example `psql -v owner_pw=CHANGE_ME_OWNER -v app_pw=CHANGE_ME_APP --dbname postgres -f setup.sql`. Replace `CHANGE_ME_OWNER` and `CHANGE_ME_APP` with long random hex strings (they end up in URLs).

<!-- sql:setup -->
```sql
CREATE ROLE braid_owner LOGIN BYPASSRLS PASSWORD :'owner_pw';
CREATE ROLE braid_app LOGIN NOBYPASSRLS PASSWORD :'app_pw';
CREATE DATABASE braid OWNER braid_owner;
REVOKE ALL ON DATABASE braid FROM PUBLIC;
GRANT CONNECT ON DATABASE braid TO braid_app;
```

This is exactly what `deploy/postgres-init.sh` does. No per-table SQL is needed: the migrations run as `braid_owner` create the tables and functions and grant `braid_app` only what it needs.

The owner role needs `BYPASSRLS` and the right to run `CREATE EXTENSION pg_trgm` (or an admin creates the extension in the `braid` database beforehand); check that your Postgres setup allows both.

## Connection settings

```
BRAID_DATABASE_URL=postgres://braid_app:<app password>@db.example.com:5432/braid?sslmode=verify-full
BRAID_DATABASE_OWNER_URL=postgres://braid_owner:<owner password>@db.example.com:5432/braid?sslmode=verify-full
```

`BRAID_DATABASE_URL` is the runtime connection, `BRAID_DATABASE_OWNER_URL` is used for migrations only. Use `sslmode=verify-full` for anything that is not on the same host. With a custom CA add `&sslrootcert=/path/to/ca.pem` (the file must be readable inside the container).

## Compose override

The repository ships this as `deploy/docker-compose.external-db.yml`. It keeps the bundled `postgres` service from starting and removes the dependency on it:

```yaml
services:
  postgres:
    profiles: ["bundled"]
  braid:
    depends_on: !reset {}
    environment:
      BRAID_DATABASE_URL: ${BRAID_DATABASE_URL:?}
      BRAID_DATABASE_OWNER_URL: ${BRAID_DATABASE_OWNER_URL:?}
```

Put `BRAID_DATABASE_URL` and `BRAID_DATABASE_OWNER_URL` into `.env` and leave the other values from `.env.example` in place (Compose still reads them for the disabled service). Check the result with `docker compose -f docker-compose.yml -f docker-compose.external-db.yml config`, then start with the same two `-f` flags.

## Verify

```
docker compose -f docker-compose.yml -f docker-compose.external-db.yml up -d
docker compose logs braid
curl -fsS http://localhost:8080/healthz
```

The server runs the migrations on start; a successful start and `ok` from `/healthz` (which also checks the database) mean it works.

## Backups and upgrades

Back up as the owner, and test a restore into a scratch database now and then:

```
pg_dump --format=custom --no-owner "$BRAID_DATABASE_OWNER_URL" > braid.dump
createdb braid_restore_test && pg_restore --no-owner --dbname braid_restore_test braid.dump
```

To upgrade: take a backup, pull the new image (`docker compose pull`), start it (`docker compose up -d`). Migrations run automatically on start. Check `/healthz` and the version in `/api/v1/about` (shown in the web app footer).
