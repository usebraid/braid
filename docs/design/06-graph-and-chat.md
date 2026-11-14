# Sub-project 6 – Graph and chat views

Status: draft · 2026-11-13

## 1. Why

The dashboard shows sessions as rows and as a canvas, but the owner reads Claude Code work as a conversation over time. Two things are missing on the way to v1:

- An actual timeline. Today's Timeline is a table; it does not show *when* work started or was picked up again across devices.
- The conversation itself. A session (live or finished) should read like the chat in Claude Code, and it should show when a subagent branched off and when it came back.

Success: the owner opens the Timeline and sees at a glance what started or resumed when; opens a session and reads it like the Claude Code chat, with subagents visible as branches; opens a live session and watches the chat grow.

## 2. Decisions the owner made

- **Timeline primary view = one vertical line** (mock-up "A"), newest at the top. Every dot is a moment a session **started** or was **resumed**, across all devices and projects, with date and time. Day separators between days. The existing table stays as the secondary view (toggle, like Canvas | List on the session page).
- **Session view = graph and chat in one column** (mock-up "A", like `git log --graph` with text): a top-to-bottom graph on the left, and in the same row the chat entry that happened at that point. The main session is the trunk; a subagent branches off at the tool call that spawned it and merges back when its result returns. Dots are chat messages, tool calls, etc. Canvas and List stay as further views: toggle **Graph | Canvas | List**, Graph is the default.
- **Live = the same view.** Opening a live session shows this chat; new entries appear at the bottom.
- The look of the chat follows Claude Code (accepted as proposed): your prompts set off as a block, replies as plain text, tool calls as compact one-liners, thinking and tool results collapsed by default and expandable.
- Accepted as proposed for the Timeline: colour = project, under the title project, device and model; status badges (live, failures) on the dot; filled dot = started, ring = resumed; click opens the session.
- Accepted as proposed for live: the view follows the bottom while the reader has not scrolled up; a pulsing dot marks the active lane.

## 3. Goals, non-goals, anti-goals

Goals
- Timeline graph over the current filters (range, device, project, model, skill), infinite scroll like today's table.
- Session graph+chat for every session, finished or live, including nested subagents (a subagent of a subagent branches off its parent's lane).
- Subagent steps can be collapsed to their branch-off and merge-back rows.
- Deep links: `?view=` for the toggle and a link to a single entry (the existing `focusLine` mechanism), so a search hit or failure opens the graph at that entry.

Non-goals
- Writing to Claude Code (no sending prompts, no approvals). Read-only.
- Replacing Canvas or List; they stay unchanged.
- Rendering tool results richly (diffs, images); collapsed text with the existing truncation is enough for this sub-project.

Anti-goals (a failure even if it works)
- A slow or janky page for big sessions (the fixture "Release run with 102 subagents", real sessions of 10k+ lines). The view must page and render only what is visible.
- Showing content to anyone but the session's owner, or logging line content (existing rules: RLS per user, logs never contain line content).
- Breaking the zero-token rule: nothing calls a model.

## 4. Constraints

- Everything is derived from the raw lines already stored (spec 02); no daemon or protocol change. A new derived column or table is fine and must be rebuildable by `reindex`.
- Same stack and look as spec 03 (React, ponytail tokens, JetBrains Mono for numbers). No new chart or graph library unless the plan justifies it; SVG drawn by hand like the existing charts.
- API changes go through `api/openapi.yaml` and the generated types; new endpoints follow the existing `/api/v1/stats/...` patterns, auth and RLS.
- Accessibility as in spec 03: the graph is decoration next to readable rows; every row is reachable by keyboard and reads sensibly without the graph.
- Live updates use the existing live WebSocket events and debounce.

## 5. Definitions (builder's calls, marked)

- **Resumed** (my call): a session counts as resumed at the first user prompt after the session was idle for more than 30 minutes, the same threshold the active-time rule uses (spec 02 §4). A prompt from a different device than the previous one also counts as a resume. The start is the session's first line.
- **Branch-off / merge-back** (my call): branch-off is the timestamp of the spawning tool call (`spawn_tool_use_id` of the agent), merge-back is the timestamp of its tool result in the parent; without a result (still running or killed) the branch stays open to the agent's last line, drawn as open.
- **Idle** (my call): reuse the active-time rule of spec 02 §4 exactly. A gap counts as idle when that rule does not count it as active (over 30 minutes, not covered by a tool result that reports its own duration). So a 40-minute build is not idle, and the prompt after it is not a resume.
- **Attribution of a dot** (my call): each start or resume event carries its own device (of the prompt line), model (of the first reply after it) and the session's project. Filters match the event, not the whole session: with the device filter "Linux", a session started on the Mac and resumed on Linux shows only the resume dot.
- **Where events come from** (my call): start and resume events are derived at ingest into a derived table, rebuildable by `reindex`, so the Timeline query does not scan lines. A live session produces its resume dot as soon as the line arrives; the Timeline refreshes on the existing live events with the existing debounce.
- **Order and paging of the session chat** (my call): lines of all agents of a session are ordered by (timestamp, line id) and paged with that pair as the cursor. Lines that arrive late (a subagent synced after its parent) appear when the view refetches on the live event, in their time position.
- **Topology** (my call): the client builds the lanes from the agent list of the existing session detail (complete up front, also for 102 agents: branch-off and merge-back times per agent come with it). Only the chat lines are paged. The detail gains the merge-back timestamp per agent if it does not have it yet.
- **Collapsing** (my call): subagents are expanded by default; each branch can be collapsed to its branch-off and merge-back rows with a step count, and there is a "collapse all subagents". A deep link to a line inside a collapsed branch expands that branch.
- **Noise** (my call): the server pages all lines of the session; the client hides what `transcript.ts` hides. A page may therefore show fewer entries than its size; the client loads the next page when the visible part runs short.
- **Chat entries**: the entry kinds that `web/src/lib/transcript.ts` already distinguishes (prompt, reply, thinking, tool call, tool result, system, event). Hidden or noise lines stay as they are classified there.

## 6. Left to the builder

- Endpoint shape for the timeline events and for the interleaved lines of all agents of one session (paging by time cursor is expected), and whether resume points are derived at ingest or at query time.
- Lane assignment for the session graph (how parallel subagents get columns, how many columns before lanes are compressed).
- Virtualisation technique, as long as big sessions stay smooth.
- Exact visuals within the ponytail look, and mobile behaviour (the graph may collapse to a thin rail on narrow screens).

## 7. Tests

- Derivation: resume detection (idle gap, device switch, no false resume inside a long tool run), branch/merge times including nested and unfinished agents; covered by `derivetest` scenarios and `reindex` equality.
- API: paging stability, filters, RLS isolation (another user's session is a 404).
- Web: unit tests for lane layout and entry grouping; e2e with the web harness fixtures for Timeline graph, session graph+chat, collapsing a subagent, deep link to an entry, and a live session receiving new lines.
- Performance: the 102-subagent fixture scrolls without long tasks over 50 ms in the e2e run.
