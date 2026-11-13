# Sub-project 1 – Data path

Status: draft · 2026-11-11 · Parent: [`00-overview.md`](00-overview.md)

## 1. Outcome

Every transcript file of every device ends up on the server byte-exact, exactly once, behind authentication, separated per user.

Done when:

- `braid login`, `braid install` and `braid import` bring all existing and new transcripts of a device onto the server.
- The chaos end-to-end test (section 9) passes: no line lost, none duplicated.
- Two users on one instance cannot see each other's data through any endpoint or SQL query run as the app role.
- All checks in `scripts/check.sh` pass in CI.

Out of scope: derived tables, stats, dashboard views (sub-projects 2–3). The device approval page is a minimal server-rendered page; it is restyled in sub-project 3.

## 2. Repository scaffold (first tasks)

- `git` repo `usebraid/braid`, Go module `github.com/usebraid/braid` at the root.
- `LICENSE` (AGPL-3.0), `THIRD_PARTY_NOTICES.md`, `CREDITS.md`, `README.md` (with **Special thanks**), `SECURITY.md`, `CONTRIBUTING.md` (DCO).
- `scripts/check.sh` runs: `gofmt` check, `golangci-lint` (incl. `gosec`), `govulncheck`, `go test ./...`, SPDX header check, `gitleaks`. TS tools (`biome`, `knip`, `tsc`, dependency audit) are added with the first TS package.
- `lefthook`: `check.sh` on pre-commit, Conventional Commit + `Signed-off-by` check on commit-msg.
- GitHub Actions: `check.sh`, Postgres service for integration tests, commit lint, DCO check, `trivy` on the server image. Dependabot. release-please.
- Dependencies are pinned exactly; each new one is justified in the PR.

## 3. Sync protocol

Shared types live in `internal/protocol`. Protocol version `1`, sent as header `Braid-Protocol: 1`. On mismatch the server answers `426` with `{"error":"protocol_mismatch","server":N,"min":M}` and the daemon tells the user to update.

### 3.1 Files

Root: `$CLAUDE_CONFIG_DIR/projects`, default `~/.claude/projects`. All paths sent are relative to it, with `/` separators.

| Kind | Match | Sync unit |
|---|---|---|
| `append` | `*.jsonl` | complete lines (terminated by `\n`) from the last acked offset |
| `snapshot` | `*.json` | whole content whenever its SHA-256 changes |

Everything else (e.g. `memory/`) is ignored.

**File identity:** `(device, rel_path, head_hash)`. `head_hash` is the SHA-256 of the first complete line of an `append` file, and empty for `snapshot` files. An `append` file without a complete line is not synced yet.

**New generation:** if the file's first line changes, the `head_hash` differs and it is a new file on the server, synced from offset 0. If the file got shorter than the local offset but the head is unchanged, the daemon resends from offset 0; the server dedupes and keeps changed lines as additional rows.

A partial last line (no trailing `\n`) is never sent.

### 3.2 `POST /v1/ingest`

Auth: device token with scope `ingest:write`. Body: JSON, `Content-Encoding: gzip`. Limits: 8 MiB compressed, 64 MiB decompressed (`413` otherwise).

```json
{
  "files": [
    {
      "kind": "append",
      "path": "-Users-x-proj/0b1c….jsonl",
      "head_hash": "<hex sha256>",
      "from_offset": 10240,
      "scope": "device",
      "lines": [{"text": "{\"type\":\"user\",…}"}, {"b64": "…"}]
    },
    {
      "kind": "snapshot",
      "path": "-Users-x-proj/0b1c…/subagents/agent-a1.meta.json",
      "content": {"text": "{…}"}
    }
  ]
}
```

- `scope` is optional: absent means the per-user identity, `"device"` the device-scoped identity after a `conflict`.
- A line is sent as `text` when it is valid UTF-8, otherwise as `b64`. The `\n` is not included.
- Offsets are byte offsets: line *i* starts at `from_offset + Σ(len(line_j)+1)` for *j < i*.

Response `200`:

```json
{
  "files": [
    {"path": "…", "head_hash": "…", "status": "ok",  "end_offset": 20480},
    {"path": "…", "head_hash": "…", "status": "gap", "end_offset": 8192}
  ]
}
```

- `ok`: everything in the entry is stored. `end_offset` is the end of this entry; snapshots return `0`.
- `conflict`: a line of this entry differs from the line stored at the same offset for this file identity. Nothing of this entry is stored; the daemon switches the file to `scope: "device"` (see the amendment in 3.4) and sends it again.
- `gap`: `from_offset` is greater than the server's highest stored end for this file. Nothing of this entry is stored. The daemon resends from `end_offset`.
- The whole request runs in one transaction. Any other error means nothing was stored; the daemon retries with backoff.

**Amendment (2026-11-13): oversized lines are sent in parts.** A line or snapshot longer than `ChunkSize` (1 MiB) goes as chunks: each is a `Line` with `part: {index, count, total_len, sha256}` (sha256 of the whole line), sent alone as the only line of its `FileBatch` in a request of its own, so a request stays within the body limits even at worst-case JSON escaping. The server keeps chunks in `raw_line_parts` (same RLS as `raw_lines`, keyed per uploading device on `(user_id, source_file_id, offset, device_id, idx)`) and answers `pending`; with the last chunk it assembles exactly the indexes 0..count-1 of that device's series (same sha256, count and total_len; parts of another series at the offset are deleted, never concatenated), verifies length and hash (400 on mismatch, nothing stored), stores it through the normal path and deletes the parts. Parts older than 7 days are deleted by the periodic derive sweep (`gc_line_parts()`). Derive and the line viewer never load stored lines over 64 MiB (`protocol.MaxReadLine`): derive counts them in `derive_errors`, the viewer returns a placeholder with offset and size. Chunks are concatenated, length- and hash-checked and stored inside Postgres; they never pass through server memory. Pending parts are capped at 1 GiB per user (413 with `Retry-After`, the daemon retries later); chunks of append lines must not contain `\n`. A line or snapshot over 256 MiB (`protocol.MaxLineBytes`) is the only case the daemon skips: it records `(path, offset, length, sha256)` locally, advances with `allow_gap` set on later batches of that file, keeps the spool copy and lists the entry in `braid status`.

### 3.3 `GET /v1/files`

Auth: device token. Returns `{files: [{path, head_hash, kind, scope, end_offset, content_hash, tail_offset, tail_hash}], next}` for the calling device, one cursor page at a time (`?after=<file id>&limit=<n>`, default 500, max 1000; `next` is the cursor for the following page and absent on the last; the daemon loops until it is absent); `scope` is absent (per user) or `device`, `tail_offset` and `tail_hash` give offset and SHA-256 of the stored line with the highest offset of an append file. On resume the daemon trusts `end_offset` only if its local file (or spool) holds exactly that line at `tail_offset`; otherwise it ignores the entry and resends. The daemon uses it when its local state is missing (reinstall, new state DB) to resume without resending everything. Resending is still correct, only slower.

### 3.4 Dedupe

- `raw_lines` unique on `(source_file_id, "offset", line_hash)`, inserted with `ON CONFLICT DO NOTHING`.
- `raw_snapshots` is append-only: a row is inserted only when `content_hash` differs from the newest row of that file, so a revert (v1 → v2 → v1) is recorded.
- `line_hash` and `content_hash` are SHA-256 of the raw bytes, computed by the server.

**Amendment (2026-11-12): store once per user.** File identity is `(user_id, rel_path, head_hash)`, not per device. Origin stays visible without storing content twice: every `raw_lines` and `raw_snapshots` row keeps the `device_id` that delivered it first (a 16-byte reference, not a copy), so the dashboard can show which device a session, agent or line came from, including files that several devices appended to. `GET /v1/files` returns the user's files, so a new device token for the same user (re-login, reinstall, home directory synced between machines) resumes at the existing offsets and sends nothing twice. If a device sends a line at an offset that already holds a *different* line for that identity (two machines appending to the same synced file), the server answers `conflict` for that file and the daemon falls back to a device-scoped identity `(user_id, device_id, rel_path, head_hash)` for it — the rare case where storing separately is the only lossless option.

## 4. Daemon

Single binary `braid`, runs in user context.

### 4.1 CLI

| Command | Behaviour |
|---|---|
| `braid login <server-url>` | Device flow (section 6.2), stores server URL and token. If no OS keychain is available, asks before falling back to a `0600` file. |
| `braid logout` | Revokes the token on the server (best effort) and deletes it locally. Local sync state is kept. |
| `braid run` | Foreground sync loop; what the service runs. |
| `braid install` / `uninstall` | Registers / removes the user service and starts / stops it. |
| `braid import` | If the service runs: shows backlog progress until it reaches zero. Otherwise: runs one full sync pass in the foreground. |
| `braid status` | Server URL, login state, connection, backlog (files, bytes), last successful sync, warnings. Reads the state DB directly. Exit code `0` healthy, `1` warning, `2` error. |
| `braid stats --json` | Placeholder in this sub-project; implemented in sub-project 2 for the band. |
| `braid version` | Version, commit, protocol version. |

Subcommands use the standard library (`flag`), no CLI framework.

### 4.2 Packages (`internal/…`)

| Package | Responsibility |
|---|---|
| `protocol` | Request/response types, version constant, offset arithmetic. Shared with the server. |
| `scan` | Walk the root, classify `append` / `snapshot` / ignored. |
| `watch` | `fsnotify` events plus a full rescan every 60 s; emits "file may have changed". |
| `state` | SQLite (`modernc.org/sqlite`, WAL). Holds per-file progress and settings. |
| `spool` | Hardlinks for files not fully acked (section 4.4). |
| `sync` | Read new lines, build batches (≤ 1 MiB compressed or 5,000 lines), upload, advance state only on `ok`. |
| `client` | HTTP client, gzip, auth header, protocol header, exponential backoff (1 s → 5 min, jitter). |
| `auth` | Device flow client, token storage (`go-keyring`, file fallback). |
| `service` | launchd LaunchAgent plist, `systemd --user` unit, Windows logon scheduled task (`schtasks`, current user, no admin rights). |

### 4.3 State DB

Location: OS user data dir (`~/Library/Application Support/braid`, `$XDG_STATE_HOME/braid`, `%LOCALAPPDATA%\braid`).

```sql
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
-- keys: server_url, device_id, token_store ('keychain'|'file')

CREATE TABLE files (
  rel_path      TEXT NOT NULL,
  head_hash     TEXT NOT NULL,           -- '' for snapshots
  kind          TEXT NOT NULL,           -- 'append' | 'snapshot'
  acked_offset  INTEGER NOT NULL DEFAULT 0,
  content_hash  TEXT,                    -- last acked snapshot hash
  file_id       TEXT,                    -- inode / file index
  size_seen     INTEGER NOT NULL DEFAULT 0,
  spool_path    TEXT,
  original_gone INTEGER NOT NULL DEFAULT 0,
  last_ack_at   TEXT,
  PRIMARY KEY (rel_path, head_hash)
);
```

A file lock in the data dir ensures only one process syncs at a time.

### 4.4 Spool

Protects data Claude Code deletes (after `cleanupPeriodDays`) before it is acked.

- Spool dir: `<data dir>/spool/<rel_path>`.
- When a file is seen and not fully acked, the daemon creates a hardlink in the spool. If hardlinking fails (other volume, unsupported filesystem), it copies the file instead and keeps the copy up to date while the original exists.
- The daemon reads from the original while it exists, otherwise from the spool.
- If the original's file ID changes (replaced, not appended), the old link is kept until its content is fully acked; then it is replaced by a link to the new file.
- A spool entry is removed when the original is gone and the spool content is fully acked.
- On Windows, files are opened with share-delete and handles are closed after each read, so Claude Code can always delete or rename them.

### 4.5 Warnings

`braid status` (and later the device page) warns when:

- the device has not acked anything for more than 7 days while the backlog is not empty;
- the token is rejected (`401`): "run `braid login` again". Collection and spooling continue.

## 5. Server

Single binary `braid-server`, Go, `pgx`, `coreos/go-oidc`, `golang.org/x/oauth2`, `net/http` routing (Go 1.22+ patterns). Migrations are embedded SQL files applied at start by the owner role, tracked in `schema_migrations`.

### 5.1 Configuration (environment)

| Variable | Default | Meaning |
|---|---|---|
| `BRAID_PUBLIC_URL` | required | External base URL, used for OIDC redirect and device verification URI. |
| `BRAID_LISTEN` | `:8080` | Listen address. |
| `BRAID_DATABASE_URL` | required | Connection as `braid_app`. |
| `BRAID_DATABASE_OWNER_URL` | required | Connection as `braid_owner`, for migrations only. |
| `BRAID_OIDC_ISSUER` | required | Issuer URL (discovery). |
| `BRAID_OIDC_CLIENT_ID` / `_SECRET` | required | Confidential client. |
| `BRAID_OIDC_SCOPES` | `openid profile email groups` | Requested scopes. |
| `BRAID_OIDC_GROUPS_CLAIM` | `groups` | Claim holding group names. |
| `BRAID_OIDC_ADMIN_GROUP` | `braid-admins` | Members are admins. |
| `BRAID_OIDC_ALLOWED_GROUPS` | empty | Comma-separated; empty allows every user of the provider. Admins are always allowed. |
| `BRAID_TRUSTED_PROXIES` | empty | CIDRs whose `X-Forwarded-For` is trusted for the client IP. |

No domain or provider is hard-coded. One hostname is enough; operators may split `/v1/*` (daemon traffic) from the rest at their proxy.

### 5.2 Endpoints

| Method + path | Auth | Purpose |
|---|---|---|
| `GET /healthz` | none | Liveness + DB reachability, no details. |
| `GET /auth/login` | none | Starts OIDC (code + PKCE, `state`, `nonce`). |
| `GET /auth/callback` | none | Completes OIDC, applies group rules, creates/updates user, sets session. |
| `POST /auth/logout` | session | Deletes session; RP-initiated logout if the provider supports it. |
| `GET /api/v1/me` | session | Current user, `is_admin`. |
| `GET /api/v1/devices` | session | Own devices with last sync, backlog warning. |
| `PATCH /api/v1/devices/{id}` | session | Rename. |
| `DELETE /api/v1/devices/{id}` | session | Revoke device and its tokens. |
| `GET /api/v1/audit` | session | Own audit entries, cursor pagination. |
| `GET /api/v1/admin/users` | admin | Users with device count, stored bytes, last sync. |
| `POST /api/v1/admin/users/{id}/disable` / `enable` | admin | Disable revokes all tokens and sessions. |
| `DELETE /api/v1/admin/users/{id}` | admin | Deletes user and all data; requires `?confirm=<user email>`. |
| `GET /api/v1/admin/audit` | admin | Instance-wide audit log. |
| `POST /v1/device/code` | none, rate-limited | RFC 8628 device authorization request. |
| `POST /v1/device/token` | none, rate-limited | RFC 8628 token polling. |
| `GET /device` | none (entry page), then session | Code entry (prefilled from `user_code` query) and confirmation. |
| `POST /device/approve` / `deny` | session | Approve with device name, or deny. |
| `POST /v1/device/revoke` | device token | Used by `braid logout`. |
| `POST /v1/ingest` | device token `ingest:write` | Section 3.2. |
| `GET /v1/files` | device token | Section 3.3. |

The OpenAPI spec for `/api/v1/*` is written by hand in `api/openapi.yaml` and is the source for the web app's TS types (sub-project 3).

All state-changing requests authenticated by cookie must carry an `Origin` header matching `BRAID_PUBLIC_URL`. Responses set `Content-Security-Policy`, `X-Content-Type-Options`, `Referrer-Policy`, `frame-ancestors 'none'`. Logs never contain tokens, codes or line contents.

## 6. Authentication

### 6.1 Web (OIDC)

- Authorization code + PKCE (S256), `state` and `nonce` checked, ID token verified against the issuer's JWKS.
- On every login: read the groups claim; reject if `BRAID_OIDC_ALLOWED_GROUPS` is set and the user is in none of them and not an admin; set `is_admin` from `BRAID_OIDC_ADMIN_GROUP`. A missing claim means no groups. There is no "first user becomes admin" rule.
- Disabled users are rejected.
- Session: 32 random bytes in cookie `__Host-braid_session` (`HttpOnly`, `Secure`, `SameSite=Lax`, `Path=/`); only its SHA-256 is stored. Sliding expiry 7 days, absolute 30 days.

### 6.2 Devices (RFC 8628)

1. `POST /v1/device/code` with `{hostname, os, arch, os_user, daemon_version}` returns `device_code` (32 random bytes, base64url), `user_code` (8 chars from `BCDFGHJKLMNPQRSTVWXZ`, shown as `XXXX-XXXX`), `verification_uri`, `verification_uri_complete`, `expires_in: 600`, `interval: 5`. Only hashes are stored, together with client info and requesting IP.
2. `/device` requires a web session. The code is prefilled from `verification_uri_complete`. The confirmation page shows hostname, OS, architecture, OS user, requesting IP, request time and daemon version. The user names the device and approves or denies.
3. `POST /v1/device/token` returns `authorization_pending`, `slow_down` (polling faster than `interval`, interval grows by 5 s), `expired_token`, `access_denied`, or on success `{access_token, token_type: "Bearer", scope, device_id}`. A device code is redeemable once.

Device token: `braid_dev_` + 32 random bytes base32 (lowercase, no padding). Stored as SHA-256 only. Scopes `ingest:write stats:read:self`. Expires after 90 days without use; revocable any time. No refresh tokens.

Token storage on the device: `go-keyring` (service `braid`, account = server URL). Without a keychain (e.g. headless Linux), `braid login` asks for confirmation and then writes `<config dir>/braid/token` with mode `0600`.

### 6.3 Rate limits

In-memory token buckets (per server instance):

| Target | Key | Limit |
|---|---|---|
| `/auth/*` | IP | 30 / min |
| `POST /v1/device/code` | IP | 10 / min |
| code entry on `/device` | session and IP | 5 / min per session, 20 / min per IP |
| `POST /v1/device/token` | device code | enforced via `interval` / `slow_down` |
| `/v1/ingest`, `/v1/files` | device | 600 / min |

Exceeding a limit returns `429` with `Retry-After`.

## 7. Database

Postgres 16+. Two roles: `braid_owner` owns all objects and runs migrations; `braid_app` has `SELECT/INSERT/UPDATE/DELETE` where needed, owns nothing and has no `BYPASSRLS`.

### 7.1 Tables

```sql
users (
  id uuid PRIMARY KEY, oidc_issuer text NOT NULL, oidc_sub text NOT NULL,
  email text, name text, is_admin boolean NOT NULL DEFAULT false,
  disabled_at timestamptz, created_at timestamptz NOT NULL, last_login_at timestamptz,
  UNIQUE (oidc_issuer, oidc_sub))

web_sessions (
  id_hash bytea PRIMARY KEY, user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
  created_at timestamptz NOT NULL, last_seen_at timestamptz NOT NULL, expires_at timestamptz NOT NULL)

devices (
  id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
  name text NOT NULL, hostname text, os text, arch text, os_user text, daemon_version text,
  created_at timestamptz NOT NULL, last_seen_at timestamptz, last_sync_at timestamptz, revoked_at timestamptz)

device_codes (
  device_code_hash bytea PRIMARY KEY, user_code_hash bytea NOT NULL UNIQUE,
  client_info jsonb NOT NULL, ip inet, status text NOT NULL,      -- pending|approved|denied|redeemed
  user_id uuid REFERENCES users ON DELETE CASCADE, device_id uuid REFERENCES devices ON DELETE CASCADE,
  last_poll_at timestamptz, interval_s int NOT NULL DEFAULT 5,
  created_at timestamptz NOT NULL, expires_at timestamptz NOT NULL)

device_tokens (
  id uuid PRIMARY KEY, device_id uuid NOT NULL REFERENCES devices ON DELETE CASCADE,
  user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
  hash bytea NOT NULL UNIQUE, scopes text[] NOT NULL,
  created_at timestamptz NOT NULL, last_used_at timestamptz, revoked_at timestamptz)

source_files (
  id bigserial PRIMARY KEY, user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
  device_id uuid NOT NULL REFERENCES devices ON DELETE CASCADE,
  rel_path text NOT NULL, head_hash text NOT NULL, kind text NOT NULL,
  end_offset bigint NOT NULL DEFAULT 0,
  first_seen timestamptz NOT NULL, last_seen timestamptz NOT NULL,
  UNIQUE (device_id, rel_path, head_hash))

raw_lines (
  id bigserial PRIMARY KEY, user_id uuid NOT NULL, source_file_id bigint NOT NULL REFERENCES source_files ON DELETE CASCADE,
  "offset" bigint NOT NULL, line_hash bytea NOT NULL, line bytea NOT NULL, received_at timestamptz NOT NULL,
  UNIQUE (source_file_id, "offset", line_hash))

raw_snapshots (
  id bigserial PRIMARY KEY, user_id uuid NOT NULL, source_file_id bigint NOT NULL REFERENCES source_files ON DELETE CASCADE,
  content_hash bytea NOT NULL, content bytea NOT NULL, received_at timestamptz NOT NULL,
  UNIQUE (source_file_id, content_hash))

audit_log (
  id bigserial PRIMARY KEY, at timestamptz NOT NULL,
  actor_user_id uuid, subject_user_id uuid, device_id uuid,
  action text NOT NULL, ip inet, user_agent text, detail jsonb)
```

- Raw content is stored as `bytea`: byte-exact, including lines that are not valid UTF-8. Derivation (sub-project 2) decodes it.
- `raw_lines` and `raw_snapshots` are append-only for `braid_app` (no `UPDATE`/`DELETE` grant). Deletion only happens through the admin "delete user" function.
- `end_offset` on `source_files` is the highest stored line end; it drives the `gap` check.

Audit actions: `login`, `login_denied`, `logout`, `device_code_requested`, `device_approved`, `device_denied`, `device_code_failed`, `device_renamed`, `device_revoked`, `token_expired`, `user_disabled`, `user_enabled`, `user_deleted`.

### 7.2 Row-level security

- RLS is enabled and forced on every table with `user_id` (and on `users` via `id`). Policy: `user_id = current_setting('app.user_id')::uuid`.
- `audit_log` policy uses `subject_user_id`. `device_codes` has no grants for `braid_app` at all; it is only reached through the functions below.
- Each request runs in a transaction that first runs `SET LOCAL app.user_id = '<id>'`. Without it, queries return nothing.
- `SECURITY DEFINER` functions (owned by `braid_owner`, fixed `search_path`) cover the cases before identity is known or across users:
  - `auth_session(id_hash)` → user id, admin flag; refreshes `last_seen_at`.
  - `auth_device_token(hash)` → device id, user id, scopes; refreshes `last_used_at`; rejects revoked, expired or disabled.
  - `upsert_oidc_user(issuer, sub, email, name, is_admin)` → user id.
  - device-code functions (create, poll, approve/deny by user code).
  - admin: `admin_list_users()` (aggregates only), `admin_set_disabled(user_id, bool)`, `admin_delete_user(user_id)`, `admin_audit(cursor)`. Each checks the caller's admin flag.
  - `write_audit(...)`.
- Admin functions never return transcript content.

## 8. Deployment for development

`deploy/docker-compose.yml`: `braid-server`, Postgres (init script creates both roles), Pocket ID. Server image: multi-stage build, distroless static base pinned by digest. Polished self-hosting docs come in sub-project 5.

## 9. Tests

All tests run in `scripts/check.sh` / CI. Integration tests need Postgres via `BRAID_TEST_DATABASE_URL`.

**Daemon unit tests**

- Partial last line is not sent; it is sent once completed.
- Head change → new identity, sync from 0. Shrink with same head → resend from 0.
- Snapshot sent only when its hash changes.
- Offsets advance only on `ok`; `gap` rewinds to the returned `end_offset`.
- Invalid UTF-8 line goes out as `b64` and round-trips byte-exact.
- Spool: original deleted before ack → data read from spool; spool entry removed after full ack; copy fallback when hardlink fails; replaced file keeps old link until acked.
- Missing local state → resume via `GET /v1/files`.
- Token store falls back to a `0600` file only after confirmation.
- Service files are generated correctly per OS (golden files).

**Server integration tests**

- Ingest: idempotent resend, gap detection, multi-file batch is atomic, size limits, wrong protocol version → `426`, wrong scope → `403`.
- RLS isolation: user A cannot read or change B's rows through any `/api/*` or `/v1/*` endpoint, nor through direct table access as `braid_app` with `app.user_id` set to A. The pre-identity definer functions (`upsert_oidc_user`, `write_audit`) refuse to act for another user in a scoped session. `braid_app` cannot `UPDATE`/`DELETE` raw tables.
- Device flow attacks: expired code, reused code, wrong user code until rate limit, polling too fast → `slow_down`, denied code, revoked token → `401`, token of a disabled user → `401`, token unused for 90 days → `401`.
- OIDC against a mock provider: bad `state`, bad `nonce`, wrong issuer, wrong audience, missing groups claim, allowlist and admin group rules, disabled user.
- Origin check on cookie-authenticated `POST`/`PATCH`/`DELETE`.
- Admin functions return no content and refuse non-admins.

**Chaos end-to-end test** (`go test -tags e2e`)

Real server and real daemon with a temporary `HOME` and a test OIDC user. A generator writes main and subagent transcripts plus snapshot files for several minutes. Meanwhile, at random:

- `kill -9` the daemon and restart it,
- stop and restart the server,
- delete original files before they are synced,
- replace a file with new content (new head).

At the end, for every file generation the generator produced, the server holds every line exactly once at the right offset with the right hash, and every snapshot version that existed for at least one sync cycle.

**Manual check:** login with Pocket ID through the compose setup, approve a device, import the real transcripts of one machine, and compare the line count per file with the local files.
