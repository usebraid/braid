---
title: Operations
order: 7
group: Run the server
description: Back up, upgrade, rebuild derived data and manage devices.
---

# Operations

## Backups

All state is in Postgres (the Pocket ID data lives in its own volume). Back up the `braid` database as the owner:

```
docker compose exec -T postgres pg_dump -U braid_owner --format=custom --no-owner braid > braid.dump
```

Restore into an empty database with `pg_restore --no-owner --dbname <db> braid.dump` and test that now and then. Raw transcript lines are the source of truth; derived tables can be rebuilt with `reindex` (below).

## Upgrades

1. Back up.
2. Pull the new image (`docker compose pull`) or rebuild (`docker compose build`).
3. `docker compose up -d`. The server runs the migrations as `braid_owner` on start.
4. Check `curl -fsS <BRAID_PUBLIC_URL>/healthz` and the version in the web app footer.

Read the release notes first; 0.x releases may change interfaces.

## Rebuild derived data

```
docker compose run --rm braid reindex
docker compose run --rm braid reindex --user <user uuid>
```

`reindex` rebuilds sessions, usage, failures and search from the raw lines, for all users or one, and prints one line per user (`<user id> <rows>`). It needs the same `BRAID_*` environment as the server.

A session that continues on a second device is numbered as its files arrive; run `reindex --user <user uuid>` once both devices have synced to number its resumes exactly.

## Source offer

The web app footer links to the source code (AGPL). It points to the upstream repository by default. If you run a modified build, set `BRAID_SOURCE_URL` to an `https` URL where your source is available (no credentials, query or fragment).

## Disk usage

Raw lines and snapshots dominate. Table sizes:

```sql
SELECT relname, pg_size_pretty(pg_total_relation_size(oid)) FROM pg_class
WHERE relkind = 'r' AND relnamespace = 'public'::regnamespace ORDER BY pg_total_relation_size(oid) DESC;
```

Stored bytes per user (run as `braid_owner`, who bypasses row-level security):

```sql
SELECT u.email, pg_size_pretty(sum(octet_length(r.line))::bigint) FROM raw_lines r JOIN users u ON u.id = r.user_id GROUP BY u.email ORDER BY sum(octet_length(r.line)) DESC;
```

The admin page of the web app shows stored bytes and last sync per user too.

## Devices and revoking access

- A user sees their devices on the Devices page and can rename or revoke them. Revoking deletes the device's tokens; the daemon reports "token rejected" in `braid status`.
- On the device itself, `braid logout` revokes the token on the server and deletes it locally.
- Admins can disable a user (revokes all tokens and sessions) or delete a user with all their data on the Admin page.

## Audit log

Logins, denied logins, device approvals, revocations and admin actions are recorded. Users see their own entries, admins the whole instance, in the web app (API: `GET /api/v1/audit`, `GET /api/v1/admin/audit`). Entries contain no tokens or transcript content.
