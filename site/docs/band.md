---
title: Claude Code band
order: 3
group: Start
description: Show session cost, today's usage and the Braid sync state above the Claude Code prompt.
---

# Claude Code band

The band is a Claude Code plugin that draws one line above the prompt. It shows what the [daemon](/docs/daemon/) already knows, so you do not have to open the dashboard.

## What the line shows

```
$0.42 │ today $3.10 · 1.2M · 4 sessions ●
```

- **This session**: its cost, left of the bar. Without a figure from Claude Code it falls back to the session's numbers from `braid stats`.
- **Today**: cost, tokens and number of sessions across all your devices.
- **The dot** is the sync state:
  - green: synced
  - yellow: a backlog is waiting; the line adds how far behind the daemon is (for example `2.0 MB behind`)
  - red: the daemon is blocked (the line says `blocked`) or the server is offline
- **`resets in 2h 05m`** appears when a usage limit is known and has not reset yet.
- **`stale`** and dimmed totals mean the last refresh failed or the server is offline; the figures are the last good ones.

If the device is not logged in, or `braid` cannot be run, and there are no earlier figures, the line reads `braid: not connected`.

## Costs and privacy

The band costs no tokens. It adds no skill, command, agent or hook, and sends nothing to the model. The only thing it runs is `braid stats --json` (with the session id), at most every 30 seconds, and it never waits for it while drawing.

## Requirements

- the `braid` CLI on the `PATH` that Claude Code runs with
- a logged-in device (`braid login`, see the [quick start](/docs/quick-start/))

## Install, update, uninstall

In Claude Code:

```
/plugin marketplace add usebraid/braid
/plugin install braid@braid
```

Update with `/plugin marketplace update braid`, or enable auto-update for the marketplace in `/plugin`. Remove it with `/plugin uninstall braid@braid`.

## Troubleshooting

- **No line at all**: the plugin is not installed or not enabled; check `/plugin`.
- **`braid: not connected`**: run `braid status` in a terminal. Exit code 2 means the device is not logged in (`braid login <server-url>`). If it works in your terminal, Claude Code does not see `braid` on its `PATH`; start Claude Code from a shell where `which braid` finds it.
- **Dot is yellow or red**: `braid status` shows the backlog and the last upload error; see [Daemon](/docs/daemon/#troubleshooting).
- **`stale`**: the last `braid stats --json` call failed. Run it yourself to see the error.
