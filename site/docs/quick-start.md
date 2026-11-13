---
title: Quick start
order: 1
---

# Quick start

This gets a Braid server, a login provider and your first device running on one machine. TLS and a reverse proxy are covered in [Reverse proxy](/docs/reverse-proxy/).

## 1. Install the daemon

```
curl -fsSL https://get.usebraid.dev | sh
```

The script verifies a SHA-256 sum before it installs anything and puts `braid` in `~/.local/bin`. You can read it first at <https://usebraid.dev/install.sh>. With Homebrew: `brew install usebraid/tap/braid`. On Windows see [the daemon page](/docs/daemon/#windows).

## 2. Run the server

You need Docker with Compose. Get the `deploy/` directory from the repository, then:

```
cd deploy
cp .env.example .env
```

Fill in the passwords and the encryption key in `.env` (the commands are in the file; use hex passwords, they end up in URLs). Browser and server must reach Pocket ID under the same name, so locally:

```
echo "127.0.0.1 pocket-id" | sudo tee -a /etc/hosts
docker compose up -d postgres pocket-id
```

Open <http://pocket-id:1411> and create the admin account. In Pocket ID:

1. Create a user group `braid-admins` and add yourself.
2. Create an OIDC client with the callback URL `<BRAID_PUBLIC_URL>/auth/callback` (locally `http://localhost:8080/auth/callback`).
3. Put the client ID and secret into `.env` as `BRAID_OIDC_CLIENT_ID` and `BRAID_OIDC_CLIENT_SECRET`.

Then start Braid:

```
docker compose up -d --build
```

To use the published image instead of building it, replace the `build:` block of the `braid` service with `image: ghcr.io/usebraid/braid-server:<version>` and run `docker compose up -d`.

Other login providers are covered in [OIDC providers](/docs/oidc-providers/); a Postgres you already run in [External Postgres](/docs/external-postgres/).

## 3. First login

Open <http://localhost:8080> and sign in with Pocket ID. The first sign-in creates your user; members of `braid-admins` are admins.

## 4. Connect a device

```
braid login http://localhost:8080
braid install
```

`braid login` prints a code and a link; approve the device in the browser. `braid install` registers the background service and starts syncing. `braid status` shows the login and the backlog.

## Verify a release

Download `checksums.txt` and `checksums.txt.sigstore.json` from the release page.

```
cosign verify-blob --bundle checksums.txt.sigstore.json --certificate-identity https://github.com/usebraid/braid/.github/workflows/release.yml@refs/heads/main --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
```

Then check the archives with `sha256sum -c checksums.txt`. The server image is signed too:

```
cosign verify --certificate-identity https://github.com/usebraid/braid/.github/workflows/release.yml@refs/heads/main --certificate-oidc-issuer https://token.actions.githubusercontent.com ghcr.io/usebraid/braid-server:<version>
```
