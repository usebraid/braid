# Braid – Overview

> Braid braids the Claude Code sessions of all your devices into one stream.
> Open source (AGPL-3.0-or-later), self-hosted. Project site: usebraid.dev

Status: draft · 2026-11-11

## 1. Goal

A self-hosted, cross-device dashboard holding the **complete, lossless history** of all Claude Code sessions:

- One timeline across all sessions and devices, filterable by device, project, model, skill and time range.
- Live view of running sessions including subagents.
- Session detail as a node canvas of the main session, its subagents and skill phases.
- KPIs (sessions, agent time, cost, tokens, streak) across all devices and the whole history.
- Failures: how often and why Claude failed — usage limits (5-hour, weekly, monthly spend), rate limits, server/network errors, refusals, failed tool calls — including time lost until a limit reset.
- Look & feel of the ponytail board: dark background, vivid palette, gradients, glow.
- A band inside Claude Code showing the current session plus "today across all devices".

Braid is only considered usable once all sub-projects (section 6) are done.

## 2. Hard requirements

1. **Nothing is lost.** Raw data is stored immutably and byte-exact. Offline phases, crashes, token expiry and local transcript cleanup by Claude Code never lose data.
2. **No token cost.** Braid must not make Claude Code sessions more expensive. Hooks are allowed when they are useful, but must add nothing (or at most a few tokens, justified in the spec) to the model context: no skills, no hook output that reaches the model. Verified by comparing `/context` with and without Braid installed.
3. **No endpoint without auth**, except `/healthz` and the device-flow entry page. The server may be publicly reachable.
4. **Import of all existing transcripts** on every device.
5. **Self-hosting first.** No built-in domain, any standards-compliant OIDC provider, `docker compose up` to start.
6. **Credit** every project we take code or ideas from (section 9).

## 3. Non-goals

- No hosted SaaS. usebraid.dev is a static project site only.
- No team features (shared dashboards, shared sessions). Multiple isolated users per instance are supported.
- No controlling agents from the dashboard; observe only.
- No end-to-end encryption in v1 (section 10).
- No hardening beyond the essentials in the app. Reverse proxy, TLS, fail2ban/CrowdSec, disk encryption and backups are the operator's responsibility; the docs give pointers, not full configurations.

## 4. Architecture

```
┌──────────── device (macOS / Linux / Windows), per OS user ─────────────┐
│ Claude Code ── writes ──> ~/.claude/projects/**                         │
│                              │ fsnotify + periodic rescan               │
│                              ▼                                          │
│               braid daemon (Go, single binary, user context)            │
│               state: SQLite · spool: hardlinks · token: OS keychain     │
│ band mod in Claude Code ── runs `braid stats --json` ──┐                │
└──────────────────────────────│─────────────────────────│────────────────┘
                               │ HTTPS, device token      │
                               ▼                          │
┌──────────────────── self-hosted server ─────────────────│──────────────┐
│ reverse proxy (operator's choice, TLS)                   │              │
│ braid-server (Go): ingest API · web API · WebSocket · static web app    │
│                    │                                                    │
│                Postgres (raw layer + derived tables, row-level security)│
│ OIDC provider (e.g. Pocket ID, Authentik, Keycloak, Authelia, Zitadel)  │
└─────────────────────────────────────────────────────────────────────────┘
```

### Components

| Component | Tech | Notes |
|---|---|---|
| Daemon `braid` | Go | Runs in user context: launchd LaunchAgent (macOS), `systemd --user` (Linux), logon scheduled task (Windows). Reads only the current user's `~/.claude`. |
| Server `braid-server` | Go, `pgx`, `go-oidc` | Same Go module as the daemon; shares `internal/protocol`. Serves the web app's static files. |
| Database | Postgres | Raw append-only layer plus derived tables that can be rebuilt (`reindex`). Row-level security per user. |
| Web app | Vite + React + React Flow | ponytail board look; ponytail's SVG charts ported as React components; a chart library only where ponytail has no equivalent. TS types generated from the server's OpenAPI spec. |
| Band | Claude Code mod (TS) | `claude-band.json` with empty `hooks` and one UI module. Gets data by running `braid stats --json`; has no token of its own. No skills. |

### Users and devices

- Web login is OIDC only (authorization code + PKCE). Braid stores no passwords and implements no WebAuthn.
- The only auth Braid implements itself is the OAuth device authorization grant (RFC 8628) for daemons.
- A **device** is one OS user on one machine. Each device has its own revocable token with scopes `ingest:write` and `stats:read:self`; it can never read history.
- Multiple users per instance, strictly isolated (Postgres RLS). Admin and login allowlist come from OIDC group claims. Admins see aggregate numbers only, never other users' content.

## 5. Repository

```
braid/
├── LICENSE                  # AGPL-3.0
├── THIRD_PARTY_NOTICES.md   # license texts of code we include
├── CREDITS.md               # ideas and references we built on
├── README.md                # incl. "Special thanks"
├── SECURITY.md
├── CONTRIBUTING.md          # DCO
├── go.mod                   # module github.com/usebraid/braid
├── cmd/braid/               # daemon + CLI
├── cmd/braid-server/        # server
├── internal/                # shared and per-binary packages
├── web/                     # dashboard (TS)
├── band/                    # Claude Code mod (plugin + marketplace)
├── deploy/                  # docker-compose, proxy examples
├── site/                    # usebraid.dev (static) + install.sh
├── scripts/check.sh         # all checks
└── docs/
    └── design/              # these specs (public)
```

Naming: GitHub `usebraid/braid`, image `ghcr.io/usebraid/braid-server`, Homebrew tap `usebraid/homebrew-tap` (formula `braid`).

Specs live in `docs/design/` (English, no private domains or paths). Implementation plans are working files and stay out of the repo (`.superpowers/` is git-ignored).

## 6. Sub-projects

Each sub-project gets its own design spec, implementation plan and execution. Order is by risk and dependency, not by intermediate usability.

| # | Sub-project | Scope | Spec |
|---|---|---|---|
| 0 | Facts & scaffold | Transcript format and mod API verified (section 8); repo scaffold, CI and checks become the first tasks of sub-project 1. | this document |
| 1 | Data path | Daemon (watch, offset/ACK, spool, import, service), ingest API, raw schema, OIDC web login, device flow, keychain, multi-user + RLS, device management, admin, audit log, basic rate limits. | `01-data-path.md` |
| 2 | Derivation | Parser → sessions, agents, tool calls, skill phases, cost and tokens, failures; `reindex`; full-text search; stats API. | `02-derivation.md` |
| 3 | Dashboard & live | All views (overview + KPIs, timeline, session canvas, live, devices, audit log, search, admin), WebSocket, light/dark. | `03-dashboard.md` |
| 4 | Band | Claude Code mod using `braid stats --json`. | `04-band.md` |
| 5 | Release & website | GoReleaser, signatures, GHCR multi-arch, Homebrew tap, `install.sh`, usebraid.dev modelled on the ponytail website, self-hosting docs. | `05-release.md` |
| 6 | Cloud sessions | Sync Claude Code sessions that run in Anthropic's cloud (claude.ai/code, web and mobile), where no daemon runs. Starts with a spike that tests two candidates: (a) a token-free hook (`SessionEnd`/`Stop`, shipped as plugin or repo settings) that uploads the transcript via the existing ingest protocol, with the cloud environment registered as its own device token; open questions: outbound network access to the Braid server and how to provide the token securely; (b) an official API to fetch cloud session transcripts, if one exists. Fallback: sessions teleported to a local terminal are already picked up by the daemon. | `06-cloud-sessions.md` |

## 7. Licensing and contributions

- AGPL-3.0-or-later for all Braid code; `SPDX-License-Identifier: AGPL-3.0-or-later` header in every own source file.
- Code taken from MIT projects keeps its copyright header; its license text goes into `THIRD_PARTY_NOTICES.md`.
- Contributions via DCO (`Signed-off-by`), no CLA.
- AGPL §13 notice in README; the web app footer links to the source of the running version (repository + commit/version).
- Conventional Commits, SemVer (0.x until 1.0), changelog and versions via release-please.

## 8. Verified facts (Claude Code 2.1.322)

- **Layout:** `~/.claude/projects/<encoded-cwd>/<session-id>.jsonl` for the main session. Subagents: `<encoded-cwd>/<session-id>/subagents/agent-<id>.jsonl` plus `agent-<id>.meta.json` (`agentType`, `description`, `toolUseId`, `spawnDepth`, `worktreePath`, `finished`, …). Also `custom-title.json` and `agent-<id>.prefix.json`. `CLAUDE_CONFIG_DIR` relocates `~/.claude`.
- **File kinds:** `.jsonl` files are append-only; `.json` files are small and rewritten in place.
- **Lines:** about 25 `type` values (`user`, `assistant`, `attachment`, `system`, `api-request`, `api-request-blob`, `ai-title`, `mode`, `pr-link`, …). Many lines carry no `uuid`, so identity is file + byte offset.
- **Subagent linkage:** subagents are spawned by the `Agent` tool; `meta.json.toolUseId` equals the `tool_use.id` in the parent transcript. Nesting is possible (`spawnDepth`).
- **Cost:** one API response spans several `assistant` lines sharing `message.id` and an identical `usage`; usage must be counted once per `message.id`. `usage` includes 5m/1h cache creation, cache reads, thinking tokens, server tool use and `speed`.
- **Failures:** API failures are `assistant` lines with `isApiErrorMessage: true`, plus `apiError` (e.g. `usage_limit_reached`, `computer_slept`), `error` (`rate_limit`, `server_error`, …), `apiErrorStatus` (HTTP status) and, for limits, `quotaLimits` / `apiErrorParams.rate_limit_info` with `rateLimitType` (e.g. `five_hour`), `resetsAt` (epoch seconds) and `overageStatus`. Refusals appear as `system` lines with subtype `model_refusal_*`. Failed tool calls have `is_error: true` on their `tool_result`.
- **Skills:** `Skill` tool calls carry `input.skill` (e.g. `superpowers-extended-cc:writing-plans`), which is enough to detect workflow phases.
- **Mod API:** `register(on, options)` with `$.http.fetch`, `$.fs`, `$.process.run`, `$.session.usage()`, `$.ui.status`; loaded via a plugin `hooks` entry pointing to a JSON file with empty `hooks` and a `modules` list.
- **Cleanup:** Claude Code deletes old transcripts after `cleanupPeriodDays`. Braid does not change this setting; the spool (see `01-data-path.md`) keeps unsynced data alive.

## 9. Credits

Braid builds on:

| Project | What | License |
|---|---|---|
| [simple10/agents-observe](https://github.com/simple10/agents-observe) (Joe Johnston) | Reference for transcript parsing, event model, hierarchy and replay UI ideas, and the original idea of syncing sessions | MIT |
| [DietrichGebert/ponytail](https://github.com/DietrichGebert/ponytail) (Dietrich Gebert) | Board look & feel and SVG charts, cost/metric logic and price table, band mod as template, website as model for usebraid.dev | MIT |

Rules:

- Code taken over: original copyright header stays in the file, license text in `THIRD_PARTY_NOTICES.md`.
- Ideas without code: one line in `CREDITS.md`.
- README has a **Special thanks** section for ponytail and agents-observe. Dashboard footer and usebraid.dev show a "Built on agents-observe & ponytail" line.
- Notable dependencies (e.g. React Flow) are named in the README.
- Improvements useful upstream are offered back as PRs under the upstream license.

## 10. Future

- End-to-end encryption: devices encrypt payloads, server sees ciphertext, derivation and search move to the browser. The schema already keeps payload and metadata separately addressable.
- Single-user mode without an external OIDC provider.
- Team views, export (JSONL, CSV), per-project retention, notifications.
- Import of claude.ai chat history (browser, not Claude Code) from the claude.ai data export: archive only, no live sync, different metrics (no tool calls or subagents).
