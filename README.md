<p align="center"><img src="site/assets/logo.svg" width="96" height="96" alt="Braid logo"></p>

<h1 align="center">Braid</h1>

<p align="center">
  <a href="https://github.com/usebraid/braid/releases/latest"><img src="https://img.shields.io/github/v/release/usebraid/braid?label=release" alt="Latest release"></a>
  <a href="https://github.com/usebraid/braid/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/usebraid/braid/ci.yml?branch=main&label=ci" alt="CI status"></a>
  <a href="https://github.com/usebraid/braid/actions/workflows/release.yml"><img src="https://img.shields.io/github/actions/workflow/status/usebraid/braid/release.yml?branch=main&label=release%20build" alt="Release build status"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/usebraid/braid" alt="License: AGPL-3.0-or-later"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/usebraid/braid" alt="Go version"></a>
  <a href="https://github.com/usebraid/braid/pkgs/container/braid-server"><img src="https://img.shields.io/badge/image-ghcr.io%2Fusebraid%2Fbraid--server-blue?logo=docker&logoColor=white" alt="Container image on GHCR"></a>
  <a href="https://github.com/usebraid/homebrew-tap"><img src="https://img.shields.io/badge/homebrew-usebraid%2Ftap-orange?logo=homebrew&logoColor=white" alt="Homebrew tap"></a>
  <a href="https://usebraid.dev/docs/"><img src="https://img.shields.io/badge/docs-usebraid.dev-informational" alt="Documentation"></a>
  <a href="https://www.conventionalcommits.org"><img src="https://img.shields.io/badge/commits-conventional-fe5196?logo=conventionalcommits&logoColor=white" alt="Conventional Commits"></a>
</p>

Braid is a self-hosted daemon and server that syncs Claude Code transcripts from your machines into one place, so sessions, subagents, costs and failures can be browsed and replayed.

## What it does

- Syncs Claude Code transcripts from every device to your own server, resuming after offline periods.
- Shows sessions, costs and tokens on a board, per project, model and device.
- Replays a session as a chat with a git-style graph of its subagents (or as a canvas), live while it runs.
- Shows a timeline of when sessions started and were resumed, across devices.
- Lists failures: what went wrong, how often and how much time it cost.
- Supports several users on one server, each seeing only their own data; login through your OpenID Connect provider.

## Install

Daemon (macOS and Linux), with the install script that checks a SHA-256 sum before installing (read it first at <https://usebraid.dev/install.sh>):

```
curl -fsSL https://get.usebraid.dev | sh
```

Windows: download the zip from the release page and follow the [manual steps](https://usebraid.dev/docs/daemon/#windows).

Server: Docker Compose, see [Quick start](https://usebraid.dev/docs/quick-start/). More in the [docs](https://usebraid.dev/docs/): external Postgres, OIDC providers, reverse proxy, operations.

## Quick start

```
braid login <server-url>
braid install
```

## Verify releases

Releases are signed with cosign (keyless). Verify the checksums file, then the archives with `sha256sum -c checksums.txt`:

```
cosign verify-blob --bundle checksums.txt.sigstore.json --certificate-identity https://github.com/usebraid/braid/.github/workflows/release.yml@refs/heads/main --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
```

The server image:

```
cosign verify --certificate-identity https://github.com/usebraid/braid/.github/workflows/release.yml@refs/heads/main --certificate-oidc-issuer https://token.actions.githubusercontent.com ghcr.io/usebraid/braid-server:<version>
```

## Status

Pre-1.0 (0.x): interfaces may change.

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md). Report vulnerabilities as described in [SECURITY.md](SECURITY.md).

## AGPL section 13 notice

Braid is licensed under AGPL-3.0-or-later. If you run a modified version as a network service, you must offer its users the corresponding source. The web app footer links to the source of the running version.

## Special thanks

- [ponytail](https://github.com/DietrichGebert/ponytail) by Dietrich Gebert: board look and feel and SVG charts, cost/metric logic and price table, the band mod as a template, and the website as a model for usebraid.dev.
- [agents-observe](https://github.com/simple10/agents-observe) by Joe Johnston: reference for transcript parsing, the event model, hierarchy and replay UI ideas, and the original idea of syncing sessions.

## Notable dependencies

- React Flow (`@xyflow/react`, MIT) and dagre (`@dagrejs/dagre`, MIT) draw the session canvas.
- `github.com/jackc/pgx/v5` (MIT): the Postgres driver.
- `github.com/coreos/go-oidc/v3` (Apache-2.0): OpenID Connect login.
- `modernc.org/sqlite` (BSD-3-Clause): the daemon's local state, pure Go.
- `github.com/fsnotify/fsnotify` (BSD-3-Clause): watching transcript files.
- `github.com/zalando/go-keyring` (MIT): token storage in the OS keychain.
- `marked` (MIT): renders the docs of this site at build time.

See [CREDITS.md](CREDITS.md) and [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## License

AGPL-3.0-or-later, see [LICENSE](LICENSE).
