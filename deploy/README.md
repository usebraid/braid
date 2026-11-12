# Braid local deployment

Postgres, Pocket ID and braid-server for local end-to-end use. TLS and a reverse proxy are the operator's job.

## Start

1. `echo "127.0.0.1 pocket-id" | sudo tee -a /etc/hosts` (browser and server must reach Pocket ID under the same name).
2. `cp .env.example .env` and fill in the passwords and the encryption key (commands in the file).
3. `docker compose up -d postgres pocket-id`, then open <http://pocket-id:1411> and create the admin account.

## OIDC client in Pocket ID

1. Create a user group `braid-admins` and add yourself. Make sure the groups claim is released (default scope `groups`).
2. Create an OIDC client with callback URL `<BRAID_PUBLIC_URL>/auth/callback` (locally `http://localhost:8080/auth/callback`).
3. Put client ID and secret into `.env` (`BRAID_OIDC_CLIENT_ID`, `BRAID_OIDC_CLIENT_SECRET`), then `docker compose up -d --build`.

## First login and device login

Open <http://localhost:8080> and sign in with Pocket ID. Then, on a device:

```
braid login http://localhost:8080
```

and approve the device in the browser.

## Hardening (not covered here)

Put it behind a TLS reverse proxy and set `BRAID_TRUSTED_PROXIES`; see `SECURITY.md` and `docs/design/01-data-path.md`. Restrict logins with `BRAID_OIDC_ALLOWED_GROUPS`. Use `sslmode=verify-full` for non-local Postgres.
