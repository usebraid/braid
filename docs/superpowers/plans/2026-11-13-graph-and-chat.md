# Graph and Chat Views Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Timeline graph of session starts and resumes, and a per-session graph+chat view (also for live sessions) with subagents as branches.

**Architecture:** Derivation writes two new derived tables: `session_events` (start/resume points) and `line_order` (every transcript line of a session with its timestamp). Two new read endpoints page them. The web app gets a pure lane/row builder, a `SessionGraph` component (graph and chat in one column) as the default session view, and a `TimelineGraph` as the default Timeline view. Canvas and List stay.

**Tech Stack:** Go 1.x + pgx + Postgres 16 (RLS), React + TypeScript + Vite, vitest, Playwright e2e with the Go web harness (`e2e/web`).

**Spec:** `docs/design/06-graph-and-chat.md` (read it first; §2 and §5 are binding).

## Global Constraints

- Everything derived from stored raw lines; no daemon or protocol change. New derived tables are emptied by `Deriver.Reset` and rebuilt by `reindex`; incremental derivation must equal a reindex at any batch border (existing golden/`reindex` equality tests must keep passing and cover the new tables).
- New tables: `user_id` column, RLS enabled + forced, policy `user_id = app_user()`, grants to `braid_app`, exactly like `internal/server/db/migrations/0005_derived.sql` lines 105–113.
- Bump `ParserVersion` in `internal/server/derive/engine.go` from 2 to 3 so existing data is rebuilt.
- Logs never contain line content, tokens or codes.
- API changes go into `api/openapi.yaml` first; web types regenerated (`npm run gen` or the existing script in `web/package.json`); hand-written fetch in `web/src/api/stats.ts`.
- No new runtime dependency (no graph or virtualisation library). SVG by hand; long lists use CSS `content-visibility: auto` with `contain-intrinsic-size` plus paging.
- Look: ponytail tokens from `web/src/theme/tokens.css`; numbers and times in JetBrains Mono; the graph is `aria-hidden`, every row is readable and keyboard-reachable without it.
- `scripts/check.sh` green before every commit; commits signed, Conventional Commits, English.

**User decisions (already made):**
- "Ich würde A präferieren" — Timeline: one vertical line, a dot per start or resume, newest at the top, day separators; the table stays as secondary view.
- "A klingt wieder gut" — Session: graph and chat in one column (`git log --graph` with text); Graph | Canvas | List, Graph default.
- Live sessions use the same view; new entries at the bottom; follows the bottom unless scrolled up.
- "Der rest passt so auch" — all proposals in spec §2 and §5 accepted (colours = project, filled = start, ring = resume, chat look like Claude Code, collapsible subagents, etc.).

## Review Focus

1. A session resumed on a second device while the first device's file is still syncing: exactly one start event, resume events stable across batch borders and equal after `reindex` → test in Task 1.
2. Lines without a timestamp (meta/system lines) must keep their place between neighbours in the chat order, not jump to the start or end → test in Task 1 (line_order) and Task 2 (chat paging).
3. A subagent that never returns (killed, or still running): branch drawn open to its last line, no crash, no phantom merge row → test in Task 3.
4. A deep link (`?line=`) to a line inside a subagent opens the graph at that line with its branch expanded → test in Task 4 and the e2e in Task 6.
5. The 102-subagent fixture: lanes capped, page stays responsive (no long task > 50 ms while scrolling) → Task 3 (cap) and Task 6 (perf).

---

### Task 1: Derive start/resume events and line order

**Goal:** Derivation fills `session_events` and `line_order`, incrementally and identical to a reindex.

**Files:**
- Create: `internal/server/db/migrations/0014_graph.sql`
- Modify: `internal/server/derive/apply.go`, `internal/server/derive/engine.go` (ParserVersion 3, Reset list, pass raw line id and device into the batch)
- Test: `internal/server/derive/graph_test.go` (scenarios via `derive/derivetest`)

**Schema (0014_graph.sql):**
```sql
CREATE TABLE session_events (
  user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
  session_id text NOT NULL,
  kind text NOT NULL CHECK (kind IN ('start', 'resume')),
  at timestamptz NOT NULL,
  device_id uuid,            -- device of the line that is the event
  model text,                -- model of the first assistant line after the event (main agent); NULL until it exists
  source_file_id bigint NOT NULL, "offset" bigint NOT NULL,
  PRIMARY KEY (user_id, session_id, kind, source_file_id, "offset"));
CREATE INDEX session_events_at ON session_events (user_id, at DESC, session_id);

CREATE TABLE line_order (
  user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
  raw_line_id bigint NOT NULL,
  session_id text NOT NULL, agent_id text NOT NULL,
  ts timestamptz NOT NULL,   -- the line's timestamp, else the previous line's of the same agent, else the file's first timestamp
  PRIMARY KEY (user_id, raw_line_id));
CREATE INDEX line_order_session ON line_order (user_id, session_id, ts, raw_line_id);
```
Plus RLS/policy/grants as in 0005. Also add to `agents`: `last_turn_device uuid` (device of the last TurnStart of the agent, for the device-switch rule).

**Rules (spec §5):**
- `start`: one per session, at the earliest timestamp of the main agent (`agent_id = 'main'`), on the line carrying it. A later batch with an earlier line moves it (keep one row: delete the old start row of the session when a new earlier one is found).
- `resume`: on a main-agent, non-sidechain `TurnStart` line when either (a) the agent's `work_prev` (the active-time state, read **before** `activeTime` resets it) is set and `e.TS - work_prev > maxGap` (30 min), or (b) `last_turn_device` is set and differs from this line's device. Never on the session's first TurnStart.
- `model`: set on the first assistant line with a model after an event of the same session; across batches fill NULL models with an UPDATE in `fixups`.
- `line_order`: every raw line of main and subagent files gets a row; `ts` as in the schema comment.

**Acceptance Criteria:**
- [ ] A session with prompts at 10:00 and 10:20 has one `start` and no `resume`.
- [ ] A prompt 45 min after the last work line gives a `resume` at the prompt's time.
- [ ] A 40-min tool run whose result reports `durationMs` 2 400 000, then a prompt 1 min later: no `resume`.
- [ ] A prompt from device B after lines from device A (5 min gap) gives a `resume` with device B.
- [ ] Event `model` equals the model of the first reply after it, also when the reply is in the next batch.
- [ ] A line without a timestamp gets the previous line's `ts`; first line without one gets the file's first timestamp.
- [ ] Deriving in batches of 1, 3 and all lines yields identical `session_events` and `line_order` to a `reindex` (extend the existing reindex-equality/golden helper).
- [ ] `Reset` empties both tables; ParserVersion is 3.

**Verify:** `BRAID_TEST_DATABASE_URL=… go test ./internal/server/derive/... ./internal/server/db/...` → ok

**Steps:**
- [ ] Write `graph_test.go` with one `derivetest` scenario per criterion above (names: `TestEventsStartOnly`, `TestEventsResumeAfterIdle`, `TestEventsNoResumeAfterReportedToolRun`, `TestEventsResumeOnDeviceSwitch`, `TestEventsModelAcrossBatches`, `TestLineOrderMissingTimestamp`, `TestGraphTablesEqualReindex`); run, see them fail.
- [ ] Add the migration; extend the batch with per-line raw id and device; implement the rules in `apply.go` next to `activeTime`; write rows in `flush`, the model fill in `fixups`; add both tables to `Reset`; bump ParserVersion.
- [ ] Run the derive and db tests plus `scripts/check.sh`; commit `feat(derive): derive session start and resume events and the line order`.

---

### Task 2: Timeline and chat endpoints, branch and merge times

**Goal:** Read API for the two new views.

**Files:**
- Modify: `api/openapi.yaml`, `internal/server/stats/sessions.go` (AgentItem), `internal/server/stats/stats.go` (routes)
- Create: `internal/server/stats/graph.go`
- Test: `internal/server/stats/graph_test.go`, extend `internal/server/stats/isolation_test.go`
- Regenerate: `web/src/api/schema.d.ts`

**Interfaces:**
- `GET /api/v1/stats/timeline?from&to&device&project&model&skill&cursor&limit` (limit default 50, max 200) → `TimelinePage { events: TimelineEvent[], next: string|null }`, newest first.
  `TimelineEvent { session_id, kind: 'start'|'resume', at, device_id: uuid|null, model: string|null, project, project_name, title, is_live, failures }`. Filters match the event: `device` on `session_events.device_id`, `model` on `session_events.model`, `project` and `skill` on the session, range on `at`. Cursor: opaque, encodes `(at, session_id, kind, source_file_id, offset)`.
- `GET /api/v1/stats/sessions/{id}/chat?after|before|at&limit` (default 200, max 500) → `ChatPage { lines: ChatLine[], next: string|null, prev: string|null }` ascending by `(ts, raw_line_id)`; `ChatLine { id, agent_id, ts, offset, line }` (line as in `LineItem`, same size cap `maxLinePageBytes`). `at=<raw line id>` returns the page starting at that line, with `prev` set. Unknown session → 404.
- `AgentItem` gains `spawned_at` and `merged_at` (both `date-time|null`): `tool_calls.started_at` / `ended_at` of the row whose `tool_use_id = agents.spawn_tool_use_id`.

**Acceptance Criteria:**
- [ ] Timeline pages are stable (no duplicates or gaps across pages when two events share `at`).
- [ ] Device filter "B" returns only the resume on B of a session started on A.
- [ ] Chat lines of main and two parallel subagents come back interleaved by time; a page border inside equal timestamps loses nothing.
- [ ] `at=` on a subagent line returns a page starting there, `prev` non-null.
- [ ] Another user's session: timeline excludes it, chat is 404 (isolation test).
- [ ] `spawned_at`/`merged_at` filled for a finished subagent, `merged_at` null for an unfinished one.

**Verify:** `go test ./internal/server/stats/...` → ok; `cd web && npm run check` → ok

**Steps:**
- [ ] Tests first (`TestTimelinePagingStable`, `TestTimelineDeviceFilterMatchesEvent`, `TestChatInterleavesAgents`, `TestChatAtLine`, `TestAgentSpawnMerge`, isolation cases), seeded like `seed_test.go`; see them fail.
- [ ] OpenAPI, handlers, routes, generated types; run tests and `scripts/check.sh`; commit `feat(api): add timeline and session chat endpoints`.

---

### Task 3: Lane layout and chat rows (pure)

**Goal:** Pure functions that turn the agent list and chat lines into graph rows.

**Files:**
- Create: `web/src/lib/lanes.ts`, `web/src/lib/chatRows.ts`
- Test: `web/src/lib/lanes.test.ts`, `web/src/lib/chatRows.test.ts`

**Interfaces:**
```ts
// lanes.ts
export type LaneAgent = { id: string; parent: string | null; from: string | null; to: string | null } // from = spawned_at ?? started_at, to = merged_at ?? ended_at
export const MAX_LANES = 6
export function assignLanes(agents: LaneAgent[]): Map<string, number> // 'main' → 0; a lane is reused once its agent has merged; beyond MAX_LANES-1 agents share the last lane

// chatRows.ts
export type Row =
  | { kind: 'entry'; key: string; lane: number; agentId: string; entry: Entry }
  | { kind: 'branch'; key: string; lane: number; from: number; agentId: string; label: string; at: string | null }
  | { kind: 'merge'; key: string; lane: number; into: number; agentId: string; at: string | null; summary: string; open: boolean }
export function buildRows(lines: ChatLine[], agents: AgentItem[], lanes: Map<string, number>, collapsed: Set<string>): Row[]
```
Branch row: emitted at the parent's tool_call entry whose `tool.id === agent.spawn_tool_use_id` (replaces that entry). Merge row: at the parent's tool_result for that id; `open: true` and no merge row when the agent has no `merged_at`. Collapsed agents: their entry rows are dropped, branch and merge rows remain with a step count in `label`. Entries come from `entriesOf` (`web/src/lib/transcript.ts`), so hidden kinds stay hidden.

**Acceptance Criteria:**
- [ ] Two sequential subagents share lane 1; two parallel ones get lanes 1 and 2; a nested one gets a new lane right of its parent.
- [ ] 102 agents never exceed `MAX_LANES` lanes.
- [ ] An unfinished subagent yields a branch row and no merge row; its entries keep its lane.
- [ ] Collapsing an agent removes its entries and keeps branch and merge rows with "N steps".

**Verify:** `cd web && npx vitest run src/lib/lanes.test.ts src/lib/chatRows.test.ts` → all pass

**Steps:** tests first, then implementation; `scripts/check.sh`; commit `feat(web): lay out agent lanes and chat rows`.

---

### Task 4: Session graph+chat view (also live)

**Goal:** `SessionGraph` renders rows as graph + chat; it becomes the default session view.

**Files:**
- Create: `web/src/components/graph/SessionGraph.tsx`, `web/src/components/graph/ChatEntry.tsx`, `web/src/components/graph/GraphCell.tsx`
- Modify: `web/src/views/Session.tsx` (Segmented Graph | Canvas | List, `?view=graph` default), `web/src/api/stats.ts` (`statsTimeline`, `statsSessionChat`), `web/src/styles/app.css`
- Test: `web/src/components/graph/SessionGraph.test.tsx`

**Behaviour:**
- Row = `[time | GraphCell (svg, aria-hidden) | ChatEntry]`. Prompt as a set-off block, reply as text, tool call one line (`Tool` + input summary), thinking and tool results collapsed (`<details>`), branch row shows agent type + description, merge row "↩ agent back after <duration>: <first line of the result>".
- Header actions: "Collapse subagents" / "Expand subagents"; each branch row has a toggle button (`aria-expanded`).
- Paging: load forward with `next` when a sentinel near the end becomes visible; `?line=<id>` loads with `at=` and expands the branch containing it, scrolls it into view and marks it (reuse `focusLine` look).
- Live (`session.is_live`): on `session_updated` for this session (existing `useLiveEvents`, debounce as in `useApi`) fetch after the last cursor; if the reader is within 80 px of the bottom, keep the bottom in view; the active lane's last dot pulses (CSS animation, off under reduced motion).
- Rows use `content-visibility: auto; contain-intrinsic-size: auto 40px`.

**Acceptance Criteria:**
- [ ] Default view of `/sessions/:id` is Graph; Canvas and List unchanged behind the toggle.
- [ ] Clicking a branch toggle hides/shows that agent's entries.
- [ ] `?line=` to a subagent line expands it and focuses that row.
- [ ] A `session_updated` event appends new rows without losing scroll position when scrolled up.

**Verify:** `cd web && npm run check` → all pass

**Steps:** component tests first (render with fixture lines; toggle; deep link; live append with a fake live context as in `api/hooks.test.tsx`), then implementation; `scripts/check.sh`; commit `feat(web): show a session as graph and chat`.

---

### Task 5: Timeline graph

**Goal:** `TimelineGraph` as the default Timeline view, the table behind a toggle.

**Files:**
- Create: `web/src/components/graph/TimelineGraph.tsx`
- Modify: `web/src/views/Timeline.tsx` (Segmented Graph | List, `?view=list`), `web/src/styles/app.css`
- Test: `web/src/components/graph/TimelineGraph.test.tsx`

**Behaviour:** one vertical line; per event a row `[time | dot | title + "project · device · model · started|resumed" + badges]`; dot colour = project colour (same mapping as the sidebar project dots), filled = start, ring = resume; day separator rows in the user's time zone (`useMe().time_zone`); live and failure badges as in `SessionRow`; the whole row links to `/sessions/:id` (resume rows add `?line=` of the event line if the API returns it — not required). Infinite scroll with `useInfinite` and `refreshOn: session_updated`.

**Acceptance Criteria:**
- [ ] Default Timeline view is the graph; `?view=list` shows today's table unchanged.
- [ ] Day separators split events by the user's time zone, not UTC.
- [ ] Start and resume rows are distinguishable without colour (text "started"/"resumed").

**Verify:** `cd web && npm run check` → all pass

**Steps:** tests first, implementation, `scripts/check.sh`, commit `feat(web): show the timeline as a graph`.

---

### Task 6: Fixtures, e2e and performance

**Goal:** The web harness has data for resumes and subagents, and Playwright covers the new views.

**Files:**
- Modify: `e2e/web/harness_test.go` (fixtures: a session resumed after 2 h on a second device; the existing golden session with subagents; the 102-subagent session), `web/e2e/views.spec.ts`, `web/e2e/live.spec.ts`, `web/e2e/a11y.spec.ts`
- Create: `web/e2e/graph.spec.ts`

**Acceptance Criteria:**
- [ ] Timeline graph shows the resumed session twice (started, resumed) with the right device names.
- [ ] Session graph shows branch and merge rows for the golden session; collapsing works; deep link to a subagent line works.
- [ ] Live: appending a line through the harness control endpoint shows a new row at the bottom.
- [ ] Scrolling the 102-subagent session to the end produces no long task > 50 ms (`PerformanceObserver` `longtask` in the page).
- [ ] axe a11y check passes on both new views.

**Verify:** `cd web && npx playwright test` (with the harness as in `site/scripts/screenshots.mjs`) → all pass

**Steps:** fixtures, specs, run, `scripts/check.sh`, commit `test(web): cover the graph and chat views end to end`. Then regenerate `site/assets/shots` with `node site/scripts/screenshots.mjs` if the session screenshot changed, commit `docs(site): refresh screenshots`.

---

### Task 7: Sidebar shows the 10 most recently active projects

**Goal:** The project list in the sidebar shows only the 10 projects with the most recent activity (owner request 2026-11-14: "Die Projektliste in der Sidebar sollte auch nur die letzten 10 … anzeigen und nicht alle").

**Files:**
- Modify: `api/openapi.yaml` (`ProjectOption.last_active: date-time`), `internal/server/stats/live.go` (`filters` handler fills it from `projects.last_seen` or the newest session end of the project), `web/src/components/Shell.tsx` (`ProjectNav`), regenerated `web/src/api/schema.d.ts`
- Test: `internal/server/stats/live_test.go` (or the existing filters test), `web/src/components/Shell.test.tsx`

**Acceptance Criteria:**
- [ ] `GET /api/v1/stats/filters` returns `last_active` per project.
- [ ] The sidebar lists "All projects" plus at most 10 projects, newest `last_active` first; "All projects" keeps the total of all projects.
- [ ] A selected project outside the 10 still appears (appended, marked selected), so the current filter is always visible.
- [ ] Project colours do not change (`projectColor` still uses the full list).

**Verify:** `go test ./internal/server/stats/...` and `cd web && npx vitest run src/components/Shell.test.tsx` → pass

**Steps:** tests first, implementation, `scripts/check.sh`, commit `feat(web): show the ten most recently active projects in the sidebar`.

---

### Task 8: Device login pages in the dashboard look

**Goal:** The server-rendered device pages (`/device` code form, confirm, approved/denied result) look like the dashboard (owner request 2026-11-14: "die Device login und Approved seite nicht im einheitlichen style").

**Files:**
- Modify: `internal/server/device/templates/device.html`, `internal/server/device/device.go` (only if the result view needs data), plus serving of a font/logo only through existing static routes
- Test: `internal/server/device/device_test.go`

**Acceptance Criteria:**
- [ ] Colours, radii, spacing and type come from the dashboard's ponytail tokens (`web/src/theme/tokens.css`), copied as CSS custom properties into the template; light and dark follow `prefers-color-scheme` like the dashboard.
- [ ] Layout: centred card with the Braid logo and wordmark as in the dashboard sidebar, title, body; buttons and inputs styled like the dashboard's (primary / secondary); code input in JetBrains Mono, upper-case, letter-spaced.
- [ ] Every view (form, confirm, approved, denied, error) uses the same shell; the approved view says the terminal will continue and the tab can be closed.
- [ ] CSP unchanged (style only via the nonce'd `<style>`; fonts/images only from same origin — use the dashboard's existing built asset URLs or inline SVG for the logo).
- [ ] Existing device tests still pass; a test asserts each view renders the shell (logo + title) and no external URL.

**Verify:** `go test ./internal/server/device/...` → ok

**Steps:** test first, template, `scripts/check.sh`, commit `feat(server): style the device login pages like the dashboard`.
