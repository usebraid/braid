---
title: Daemon
order: 2
group: Start
description: Install the braid daemon on macOS, Linux and Windows, and fix sync problems.
---

# Daemon

The `braid` daemon watches your Claude Code transcripts and uploads new lines to the server. It keeps a local state database and a spool, so it catches up after being offline.

## Install and connect

```
braid login <server-url>
braid install
```

`braid login` asks you to approve the device in the browser. The token is stored in the OS keychain; if there is none, `braid login` asks before it falls back to a file readable only by you. `braid install` registers a user service that runs `braid run`; `braid uninstall` removes it. Run `braid install` again after a new `braid login`.

## macOS

`braid install` writes a launchd agent to `~/Library/LaunchAgents/` and loads it. State lives in `~/Library/Application Support/braid`.

## Linux

`braid install` writes `~/.config/systemd/user/braid.service` and enables it with `systemd --user`. To keep it running without a login session run `loginctl enable-linger`. State lives in `$XDG_STATE_HOME/braid` (default `~/.local/state/braid`).

## Windows

Install with [Scoop](https://scoop.sh):

```
scoop bucket add usebraid https://github.com/usebraid/scoop-bucket
scoop install braid
braid login <server-url>
braid install
```

`braid install` creates a scheduled task named `Braid` that runs at logon and starts it. After `scoop update braid`, run `braid uninstall` and `braid install` so the task restarts on the new version.

Without Scoop, manual steps:

1. Download the `braid_*_windows_*.zip` for your CPU from the release page (verify it, see [Quick start](/docs/quick-start/)).
2. Unzip it and put `braid.exe` in a folder on your `PATH`.
3. In a terminal: `braid login <server-url>`.
4. `braid install` creates a scheduled task named `Braid` that runs at logon. `braid uninstall` deletes it.

State lives in `%LOCALAPPDATA%\braid`, the file-token fallback in `%APPDATA%\braid`.

## Claude Code location

The daemon reads `~/.claude/projects`. If you set `CLAUDE_CONFIG_DIR`, it reads `$CLAUDE_CONFIG_DIR/projects` instead. `braid install` copies only these variables, if they are set in your shell, into the service definition (launchd `EnvironmentVariables`, systemd `Environment=`, the Windows task): `CLAUDE_CONFIG_DIR`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`, `BRAID_DATA_DIR` and `BRAID_CONFIG_DIR`. Nothing else is copied, so server secrets in your shell never reach the service file. A value with a line break or NUL byte makes `braid install` fail. The service file is readable only by you (mode 0600); running `braid install` again fixes the mode of an older file. Change a variable, then run `braid install` again.

## Commands

| Command | What it does |
|---|---|
| `braid status` | login, backlog and warnings |
| `braid import` | sync everything now, or follow the running service until the backlog is empty |
| `braid run` | foreground sync loop (what the service runs) |
| `braid logout` | revoke the token and delete it locally |
| `braid version` | version, commit and protocol |

## Troubleshooting

`braid status` prints the server, login state, backlog and the last ack, and its exit code tells scripts what is wrong:

| Exit code | Meaning |
|---|---|
| 0 | all fine |
| 1 | warnings: no ack for more than 7 days while files are waiting, the last upload error (for example "token rejected, run `braid login` again"), or a blocked state |
| 2 | not logged in (`not logged in, run braid login <server-url>`) or the local state cannot be read |

Common cases:

- **token rejected**: the device was revoked or the token expired. Run `braid login <server-url>` again, then `braid install`.
- **backlog does not shrink**: check the server with `curl -fsS <server-url>/healthz`, then run `braid import` to see the error in the foreground.
- **server URL must use https**: `braid` only accepts `http` for localhost.
- **another braid process is syncing**: only one `braid run` can own the state directory; `braid import` then follows the running service.

To see sync state and usage inside Claude Code itself, install the [Claude Code band](/docs/band/).
