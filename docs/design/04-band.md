# Sub-project 4 – Band

Status: draft · 2026-11-12

## 1. Outcome

A band inside Claude Code that shows the current session and "today across all devices", plus the sync state of this device. It adds **no tokens** to any Claude Code session.

## 2. Packaging

- Claude Code plugin in `band/`, with a marketplace entry in the repo, modelled on the ponytail band (MIT, credited).
- `claude-band.json` with empty `hooks` and one UI module. No skills, no commands that reach the model, no hook output.
- Verification of zero token cost: `/context` with and without the plugin installed shows identical numbers; this is part of the release checklist.

## 3. Data

- **Current session (local):** `$.session.usage()` — no network.
- **Today across devices and sync state:** the module runs `braid stats --json [--session <id>]` via `$.process.run` at most every 30 s, and immediately after a session starts. The band has no token of its own.
- `braid stats --json` (daemon CLI) calls `GET /v1/stats/self` (02 §6) with the device token, caches the answer for 30 s in the data dir, and adds local state from the state DB. Output contract (stable, versioned):

```json
{
  "version": 1,
  "sync": {"state": "ok|backlog|blocked|offline|logged_out", "backlog_bytes": 0, "last_ack": "RFC 3339"},
  "today": {"cost_usd": 0.0, "tokens": 0, "sessions": 0, "agent_time_s": 0, "failures": 0},
  "session": {"cost_usd": 0.0, "tokens": 0, "failures": 0},
  "limit": {"type": "five_hour", "resets_at": "RFC 3339"}
}
```

`session` and `limit` are omitted when unknown; `limit` is the most recent unexpired usage limit of the user.

## 4. Display

- One line: session cost and tokens · today cost, tokens and sessions across devices · sync dot (green ok, yellow backlog, red blocked/offline/logged out) · limit countdown if a usage limit is active.
- Colours from the ponytail palette.
- If `braid` is not installed or not logged in: a dim "braid: not connected" hint, no errors.
- If the server is unreachable: last cached values, marked stale.

## 5. Tests

- Unit tests for formatting and state mapping of the module.
- Contract test: `braid stats --json` output validates against a JSON schema in `band/stats.schema.json`; the schema is also tested from the Go side.
- Manual: install the plugin, run a session, compare `/context` with and without the plugin.
