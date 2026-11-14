# Braid local deployment

Postgres and braid-server. You bring your own OIDC provider (set `BRAID_OIDC_ISSUER`). TLS and a reverse proxy are the operator's job.

Step-by-step instructions (install, OIDC provider, Compose, first login, device login) are in the docs: <https://usebraid.dev/docs/quick-start/>. The sources are `site/docs/` in this repository.

- External Postgres: `docker-compose.external-db.yml` is the override described in `site/docs/external-postgres.md`.
- OIDC providers, reverse proxy, operations and the daemon: `site/docs/`.
- Hardening: see `SECURITY.md` and `docs/design/01-data-path.md`.
