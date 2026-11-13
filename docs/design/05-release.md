# Sub-project 5 – Release & website

Status: draft · 2026-11-12

## 1. Outcome

Braid can be installed and self-hosted by others: signed binaries for all platforms, a signed multi-arch server image, a Homebrew tap, an install script, self-hosting docs and the project site usebraid.dev.

## 2. Releases

- release-please opens release PRs from Conventional Commits; merging one tags `vX.Y.Z` (0.x until 1.0).
- The tag triggers GoReleaser:
  - `braid` for darwin, linux and windows on amd64 and arm64, plus `braid-server` for linux amd64/arm64; archives, `checksums.txt`, SBOMs.
  - Keyless signing of checksums and image with cosign (GitHub OIDC); verification command in the docs.
  - Version injected via `-ldflags -X` into both binaries (a `version` package is added).
- Image `ghcr.io/usebraid/braid-server:<version>` multi-arch, built from the existing Dockerfile, base images pinned by digest, trivy scan must pass (HIGH/CRITICAL) before push.
- Homebrew: tap `usebraid/homebrew-tap`, formula `braid`, updated by GoReleaser. After install: `braid login <server>` then `braid install`.
- `install.sh` (served from usebraid.dev): detects OS/arch, downloads the release, verifies the checksum, installs to `~/.local/bin`, prints next steps. Windows: zip with `braid.exe`, documented manual steps (`braid login`, `braid install`).
- All GitHub Actions pinned by commit SHA; tools installed in CI pinned by version and checksum.

## 3. Self-hosting docs

Part of the site, sources in `site/docs/` (Markdown):

- Quick start with `deploy/docker-compose.yml`.
- External Postgres (instead of the bundled container), step by step:
  - Requirements: Postgres ≥ 16, extension `pg_trgm` available.
  - SQL to create the database and the two roles exactly as the migrations expect: `braid_owner` (owns the schema, runs migrations, `BYPASSRLS`, not superuser) and `braid_app` (`NOBYPASSRLS`, login), plus the grants.
  - One note for managed providers: the owner role needs `BYPASSRLS` and the right to `CREATE EXTENSION pg_trgm`; check that your provider allows both. No per-provider instructions.
  - Connection settings: `BRAID_DATABASE_URL` / owner URL, TLS with `sslmode=verify-full` and a custom CA.
  - Compose override that removes the bundled `postgres` service.
  - Verification: `braid-server` starts, migrations run, `/healthz` is green.
  - Backups and upgrades for an external database.
- OIDC setup for Pocket ID, Authentik, Keycloak, Authelia, Zitadel (client settings, group claim, `braid-admins`, allowlist).
- Reverse proxy pointers (Caddy, Traefik, nginx), `BRAID_TRUSTED_PROXIES`, TLS.
- Operations: backups (`pg_dump`), upgrades, `reindex`, disk usage, device management, revoking access.
- Hardening pointers only (fail2ban/CrowdSec, disk encryption) — operator's responsibility (00 §3).
- Daemon: install, service per OS, `CLAUDE_CONFIG_DIR`, troubleshooting (`braid status`).

## 4. Website (usebraid.dev)

- Static, modelled on the ponytail website (structure, tone, visual language); same board look as the dashboard.
- Sections: hero ("Every Claude Code session, every device, one stream"), screenshots, features, how it works (diagram from 00 §4), install, self-hosting, credits ("Built on agents-observe & ponytail"), license.
- The generator follows the ponytail website's setup if it is a static generator with permissive licence; otherwise plain HTML/CSS with a small Markdown build step for the docs. No tracking, no third-party requests.
- Hosted on GitHub Pages, deployed from `site/` by a workflow on release; `install.sh` served from the same origin.
- Domain `usebraid.dev`: DNS at Cloudflare (CNAME/A records to GitHub Pages, configured with the `cf` CLI), HTTPS enforced in GitHub Pages; Cloudflare proxy off (DNS only) so GitHub can issue the certificate.

## 5. Tests

- CI builds a snapshot release (GoReleaser `--snapshot`) on every PR.
- `install.sh` tested in CI on ubuntu and macOS runners against the snapshot artifacts.
- Link check for the site and docs.
- Manual release checklist: signatures verify, Homebrew install works, image runs with compose, band `/context` check (04).
