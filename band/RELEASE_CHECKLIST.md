# Band release checklist: zero tokens

Manual, about five minutes. Run before every release of the `braid` plugin. The band must not change what the model sees.

## 1. Preconditions

- Claude Code installed; note `claude --version`.
- `braid` installed, on PATH and logged in (`braid status` shows a device). `nvm use` is not needed.
- A scratch directory (`mkdir /tmp/band-check && cd /tmp/band-check`), outside any repo.

## 2. Baseline without the plugin

1. Start `claude` in the scratch directory. `/plugin list` must not show `braid`.
2. Run `/context` and record the totals in the table below (column "Without").
3. Send the prompt `say ok`, then `/context` again; record the Messages row ("Without, after turn").

## 3. Install

1. `/plugin marketplace add usebraid/braid` (or the path to a local checkout).
2. `/plugin install braid@braid`.
3. `/reload-plugins` or restart Claude Code.

## 4. Same numbers with the plugin

1. Start a fresh session in the same scratch directory, with the plugin enabled.
2. Run `/context` and record the same rows (column "With").
3. Pass: every row is identical to "Without": system prompt, tools, MCP tools, custom agents, memory files, skills, messages, total. The band line above the prompt is visible and is not counted.

| Row           | Without | With | Without, after turn | With, after turn |
| ------------- | ------- | ---- | ------------------- | ---------------- |
| System prompt |         |      |                     |                  |
| System tools  |         |      |                     |                  |
| MCP tools     |         |      |                     |                  |
| Custom agents |         |      |                     |                  |
| Memory files  |         |      |                     |                  |
| Skills        |         |      |                     |                  |
| Messages      |         |      |                     |                  |
| Total         |         |      |                     |                  |

## 5. A real turn

1. In the session with the plugin, send `say ok`, then `/context`; record the "With, after turn" column.
2. Pass: Messages equals "Without, after turn". The band added no message and no `additionalContext`.

## 6. Display states

Check each state visually. None may print an error or add a toast.

| State         | How to provoke it                       | Expect                                 |
| ------------- | --------------------------------------- | -------------------------------------- |
| ok            | normal run                              | green dot, session and today figures   |
| backlog       | stop the daemon, append to a transcript | yellow dot, "N KB behind"              |
| blocked       | set via a known blocked file            | red dot, "blocked"                     |
| offline       | disconnect the network                  | cached figures dim, "stale"            |
| logged_out    | `braid logout`                          | dim "braid: not connected", no figures |
| not installed | rename the `braid` binary on PATH       | same dim hint, no error                |

Restore the binary and `braid login` afterwards.

## 7. Installed plugin contents

List the installed plugin directory (under `~/.claude/plugins/`, folder of `braid`):

```
ls -R ~/.claude/plugins/cache/braid/braid/*/
```

Pass: no `skills/`, `commands/` or `agents/` directories and no `.mcp.json`; the only hook file is `hooks/claude-band.json` (same rules as `band/tests/manifest.test.ts`).

## 8. Record

Date, Claude Code version, plugin version, result (pass/fail per step), who ran it.
