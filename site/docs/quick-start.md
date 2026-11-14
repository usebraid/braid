---
title: Quick start
order: 1
group: Start
description: Run a server with a login provider and connect your first device.
---

# Quick start

This gets a Braid server, a login provider and your first device running on one machine. TLS and a reverse proxy are covered in [Reverse proxy](/docs/reverse-proxy/).

## 1. Install the daemon

```
curl -fsSL https://get.usebraid.dev | sh
```

The script verifies a SHA-256 sum before it installs anything and puts `braid` in `~/.local/bin`. You can read it first at <https://usebraid.dev/install.sh>. On Windows, install with Scoop as shown on [the daemon page](/docs/daemon/#windows).

With Homebrew (macOS, Linux) instead:

```
brew install usebraid/tap/braid
```

Homebrew asks you to trust the tap once: `brew trust usebraid/tap`. Update later with `brew upgrade braid`.

## 2. Have an OIDC provider

Braid does not ship a login provider; it assumes you already run one (Pocket ID, Authentik, Keycloak and others). In it, create a confidential client with the redirect URI `<BRAID_PUBLIC_URL>/auth/callback` (locally `http://localhost:8080/auth/callback`), a `groups` claim, and a group `braid-admins` that contains you. Details per provider: [OIDC providers](/docs/oidc-providers/).

## 3. Run the server

You need Docker with Compose. The Compose file and its helpers are in the `deploy/` directory of the repository:

```
git clone --depth 1 https://github.com/usebraid/braid
cd braid/deploy
cp .env.example .env
```

Fill in the passwords in `.env` (the commands are in the file; use hex passwords, they end up in URLs) and set `BRAID_OIDC_ISSUER`, `BRAID_OIDC_CLIENT_ID` and `BRAID_OIDC_CLIENT_SECRET` from your provider (see below). Then start Postgres and Braid:

```
docker compose up -d
```

To build the image yourself instead of pulling `ghcr.io/usebraid/braid-server`, follow the comment on the `image:` line of the `braid` service and run `docker compose up -d --build`.

A Postgres you already run is covered in [External Postgres](/docs/external-postgres/).

## 4. First login

Open <http://localhost:8080> and sign in with your provider. The first sign-in creates your user; members of `braid-admins` are admins.

## 5. Connect a device

```
braid login http://localhost:8080
braid install
```

`braid login` prints a code and a link; approve the device in the browser. `braid install` registers the background service and starts syncing. `braid status` shows the login and the backlog. Optionally, add the [Claude Code band](/docs/band/) to see cost and sync state above the prompt.

## Verify a release

Download `checksums.txt` and `checksums.txt.sigstore.json` from the release page.

```
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity https://github.com/usebraid/braid/.github/workflows/release.yml@refs/heads/main \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
```

Then check the archives with `sha256sum -c checksums.txt`. The server image is signed too:

```
cosign verify \
  --certificate-identity https://github.com/usebraid/braid/.github/workflows/release.yml@refs/heads/main \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/usebraid/braid-server:<version>
```
