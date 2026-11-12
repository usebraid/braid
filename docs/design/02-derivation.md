# Sub-project 2 – Derivation

Status: draft · 2026-11-12

## 1. Outcome

The server turns the raw, append-only layer (`raw_lines`, `raw_snapshots`) into queryable tables: sessions, agents, tool calls, skill phases, usage and cost, failures and a full-text index. A stats API exposes everything the dashboard (03) and the band (04) need. Derived tables are a cache: they can be dropped and rebuilt from raw at any time (`braid-server reindex`), and nothing in them is a source of truth.

## 2. Principles

- **Raw stays untouched.** Derivation only reads raw and writes derived tables.
- **Incremental and idempotent.** A worker keeps a cursor per source file (`derive_cursor(source_file_id, last_line_id, …)`, also the last snapshot id) and processes lines with a higher raw id after each ingest commit. The cursor follows the raw row id, not the byte offset, because a line that changes at an already seen offset (a conflict, a re-sent line) gets a new row id and would otherwise be missed. Re-processing the same lines yields the same rows (upserts keyed by natural ids).
- **Near-live.** Ingest sends `NOTIFY braid_ingest, '<user_id>'` after commit; the worker derives within seconds and then sends `NOTIFY braid_derived` for the live view (03).
- **Versioned.** A constant `parserVersion` is stored per user. When it changes, the next start rebuilds the derived tables for every user in the background; the dashboard shows "reindexing" meanwhile.
- **Lenient.** Unknown line types and unknown fields are skipped, never fatal. A line that fails to parse is counted (`derive_errors`) and skipped; the cursor advances.
- **Isolation.** Every derived table carries `user_id` and the same RLS policy as raw (`user_id = app_user()`). The worker runs per user inside `WithUser`.

## 3. Entities

| Table | Key | Content |
|---|---|---|
| `projects` | `(user_id, path)` | Working directory from the lines' `cwd`; display name = last path segment; first/last seen. |
| `sessions` | `(user_id, session_id)` | Device(s) from the lines' `device_id` (primary device = most lines), project, title (`custom-title.json` > `ai-title` line > first user prompt, 80 chars), start/end (first/last line timestamp), models used, git branch, line count. `is_live` is not stored: the API computes it at query time (last line within the past 2 minutes). |
| `agents` | `(user_id, session_id, agent_id)` | Main agent (`agent_id = 'main'`) and subagents from `subagents/agent-<id>.jsonl` + `.meta.json`: `agent_type`, `description`, `parent_agent_id`, `spawn_tool_use_id`, `spawn_depth`, start/end, `finished`. |
| `tool_calls` | `(user_id, session_id, tool_use_id)` | Agent, tool name, start (tool_use timestamp), end (tool_result timestamp), `is_error`, short input summary (≤ 200 chars, never full file contents). |
| `skill_phases` | `(user_id, session_id, agent_id, seq)` | From `Skill` tool calls (`input.skill`). A phase lasts until the next `Skill` call of the same agent or the agent's end. |
| `session_devices` | `(user_id, session_id, device_id)` | Which devices delivered lines of a session, with the line count per device; the primary device of `sessions` is the one with most lines. |
| `derive_state` | `user_id` | `parser_version` and the `rebuilding` flag of the user. |
| `derive_errors` | `(user_id, source_file_id)` | Count of unparsable lines per file and the last offset; never fatal. |
| `usage` | `(user_id, message_id)` | Counted **once per `message.id`**; a message streamed over several lines keeps the row with the greatest `output_tokens` (the final one wins, ties keep the first): model, input, output, cache read, cache write 5m, cache write 1h, thinking, server tool use, `speed`, timestamp, session, agent, cost. |
| `failures` | `(user_id, source_file_id, offset)` | See §5. |
| `search_docs` | `(user_id, source_file_id, offset)` | Only a `tsvector` (`simple` configuration) of user prompts, assistant text and tool names, plus a reference to the raw line. No copy of the text: snippets are cut from the raw line at query time (data minimisation). A `pg_trgm` index on tool names and file paths allows partial matches such as code identifiers. |

Raw data is stored once per user (01 §3.4 amendment): a re-login on the same server or a home directory synced between machines does not store a second copy, so derivation never sees duplicates.

## 4. Metrics

- **Cost:** price table ported from ponytail (MIT, credited), per model and token class, including 5m/1h cache writes and web searches. No `speed` multiplier is applied: a message with `speed = fast` is priced at the standard rate and flagged `price_estimated = true`, so its cost is an estimate, not 0. A model that is not in the table is priced as the closest known id prefix or else the newest model of its family and also flagged `price_estimated`; only a model of no known family costs 0 and is flagged `price_unknown = true`. Thinking tokens are a subset of the output tokens and are not charged twice. The table lives in `internal/server/derive/prices.go`; updates are code changes.
- **Agent time:** taken from Claude Code's own measurements, not estimated:
  - Main agent: sum of `durationMs` of all `system` lines with subtype `turn_duration` (one per turn: from the user's prompt until Claude finished).
  - Subagent, first source that exists: (1) `totalDurationMs` from the `Agent` tool result in the parent transcript (linked via `toolUseId`), or, for an asynchronously launched agent whose result only says `async_launched`, the `duration_ms` of the later task-notification line for the same `toolUseId`; (2) the subagent's own `turn_duration` lines; (3) the estimate below, flagged `duration_estimated`.
  - Session agent time = main agent time (it already contains the time spent waiting for subagents); "total agent time" additionally adds all subagent times, so parallel subagents count in parallel.
  - Fallback only for transcripts without these fields (older Claude Code versions): gaps between consecutive line timestamps, each capped at 5 minutes; such values are flagged as estimated.
- **Time lost** (KPI, failures, self stats) is the length of the union of all lost intervals, so overlapping limits and parallel sessions are not counted twice.
- **Tool time:** `durationMs` on tool results where present, otherwise tool_use → tool_result timestamps.
- **Tokens:** sum of all token classes from `usage`.
- **Streak:** consecutive days with ≥ 1 session, in the user's time zone (user setting, default from the browser on first login).
- **Day boundaries** ("today", daily charts) always use the user's time zone.

## 5. Failures

Every failure is one row with `category`, `detail`, `occurred_at`, `session_id`, `agent_id`, and for limits `limit_type` and `resets_at`.

| Category | Source |
|---|---|
| `usage_limit` | `isApiErrorMessage` with `apiError = usage_limit_reached` or `quotaLimits`; `limit_type` from `rateLimitType` (`five_hour`, `seven_day`, `monthly_spend`, …); `resets_at` from `resetsAt`. |
| `rate_limit` | `error = rate_limit` without a usage quota, also a 429 without `usage_limit_reached`/`quotaLimits`. |
| `server_error` | `error = server_error` / `apiErrorStatus` ≥ 500 / overloaded. |
| `network` | `apiError` such as `computer_slept`, connection errors. |
| `refusal` | `system` lines with subtype `model_refusal_*`. |
| `tool_error` | `tool_result` with `is_error: true`. |
| `other_api_error` | Any other `isApiErrorMessage`. |

**Time lost** for `usage_limit` = `resets_at − occurred_at`, clipped at the next successful assistant line of the same user if that comes earlier (e.g. after switching accounts); the totals are the union of the intervals (§4). Other categories have no time lost. `api_error` retry lines (Claude Code's own automatic retries) are not failures; only the final error counts. Unknown error shapes land in `other_api_error` with the raw `error`/`apiError` string as `detail`.

## 6. Stats API

All endpoints are under `/api/v1/stats`, require a web session, run in `WithUser`, accept `from`, `to` (RFC 3339) and filters `device`, `project`, `model`, `skill`; documented in `api/openapi.yaml`.

| Endpoint | Returns |
|---|---|
| `GET /overview` | KPI tiles (sessions, agent time, cost, tokens, streak, failures, time lost) plus series for the overview charts (per day: cost, tokens; per model; per project; activity heatmap hour × weekday). |
| `GET /sessions` | Timeline: cursor-paged list (`cursor`, `limit`) of sessions with device, project, title, models, start/end, cost, tokens, failure count, skills. |
| `GET /sessions/{id}` | Session detail: agents (tree), skill phases, tool-call counts per agent, usage per agent, failures. |
| `GET /sessions/{id}/lines?agent=&after=<line id>&limit=` | Paged transcript lines of one agent (raw JSON, for the side panel); `after` is the raw line id of the last line received, `next` is the cursor for the following page. |
| `GET /failures` | Failure list and aggregates per category, limit type and day. |
| `GET /search?q=` | Full-text hits with session, agent, snippet. |

The day boundaries use the user's time zone, set with `PATCH /api/v1/me` (`{"time_zone": "Europe/Berlin"}`, an IANA name; 400 `invalid_time_zone` otherwise). `GET /api/v1/me` returns `time_zone` (effective, `UTC` when unset) and `time_zone_set`.

Device token endpoint (scope `stats:read:self`): `GET /v1/stats/self?session=<id>` returns only aggregate numbers for **today** (user time zone, all devices) and, if given, the one session — never transcript content. A session counts for today when it overlaps today (it ends after the start of the day and starts before its end); agent time is the full main-agent time of these sessions, as in the overview. Used by `braid stats --json` (04).

## 7. Admin

`GET /api/v1/admin/stats` extends the existing aggregate view with, per user, the existing fields plus `sessions`, `cost_usd`, `tokens`, `last_activity_at`, `derive_errors` and `rebuilding`, and a `totals` object (`users`, `devices`, `sessions`, `stored_bytes`, `cost_usd`, `tokens`) and `rebuilding_users`. Still aggregates only.

## 8. Snapshots

Only the newest content of `.json` files is guaranteed (sub-project 1): intermediate versions overwritten while a device is offline are not recoverable. Derivation therefore always uses the latest snapshot per file.

## 9. Tests

- Golden tests: fixture transcripts (anonymised, from Claude Code 2.1.317–2.1.322, generated by builders in `derivetest`, golden dump in `internal/server/derive/testdata/golden_full.txt`, covering every line type in 00 §8, subagents, nested subagents, skills, all failure kinds) → expected derived rows.
- Idempotence: deriving the same input twice, or in random chunk boundaries, yields identical tables.
- Usage counted once per `message.id` even when split across lines and files.
- Reindex equals incremental result.
- RLS: user B never sees user A's derived rows via any stats endpoint.
- Performance: deriving 1 GB of raw lines finishes within 10 minutes on a 2-core machine; incremental derivation of a new batch within 2 s.
