# Sub-project 3 – Dashboard & live

Status: draft · 2026-11-12

## 1. Outcome

A web app, served by `braid-server`, that shows the whole history and live activity of the logged-in user: overview with KPIs, timeline, session canvas, live view, failures, search, devices, audit log and admin. It keeps the look of the ponytail board.

## 2. Stack

- Vite + React + TypeScript, React Flow for the session canvas. No other UI framework.
- Charts: ponytail's SVG charts ported as React components (MIT, credited in file headers and `THIRD_PARTY_NOTICES.md`). A chart library is added only for a chart ponytail has no equivalent for, with justification in the plan.
- API types generated from `api/openapi.yaml` (`openapi-typescript`), fetch wrapper by hand.
- Built into `web/dist` and embedded into `braid-server` via `embed`; served under `/` with SPA fallback; strict CSP (no inline scripts, `connect-src 'self'`).
- Checks: biome, knip, `tsc --noEmit`, `npm audit`, vitest; versions pinned exactly.

## 3. Look

- ponytail board: dark background, vivid palette, gradients, glow, card grid. Colours and spacing as CSS variables.
- Light mode as a second set of variables; toggle in the header, default from `prefers-color-scheme`.
- Responsive down to tablet width; the canvas is desktop-first.
- Accessible: keyboard navigation, focus rings, contrast ≥ 4.5:1 for text, charts with text alternatives (tooltip values reachable by keyboard).
- Footer: version + link to the source of the running version (AGPL §13) and "Built on agents-observe & ponytail".

## 4. Views

| View | Content |
|---|---|
| Overview | KPI tiles (sessions, agent time, cost, tokens, streak, failures, time lost); cost and tokens per day; split by model and project; activity heatmap; global filters (time range, device, project, model, skill). |
| Timeline | All sessions of all devices, newest first, infinite scroll; each row with device, project, title, models, duration, cost, skills, failure badge; same filters. Click → session canvas. |
| Session canvas | React Flow graph: main agent node, subagent nodes (edges = spawn, labelled with the spawning description), skill phases as coloured bands/groups inside an agent node, failure markers. Node click opens a side panel with the agent's transcript (paged, raw lines rendered readable: prompts, assistant text, tool calls collapsed). Auto-layout (dagre-style, top to bottom); zoom, pan, minimap. Live sessions update in place. |
| Live | Running sessions across devices (a session is live if its last line is < 2 min old), with their active agents and current tool; updates via WebSocket. |
| Failures | Counts per category and limit type over time; time lost; list with jump to the session position. |
| Search | Full-text search with snippets; click jumps to the line in the session canvas side panel. |
| Devices | Existing device API: list, rename, revoke, stale warning, last sync, backlog. |
| Audit | Own audit log (paged). |
| Admin | Only for admins: users with aggregates, disable/enable/delete, instance audit. |
| Settings | Time zone, theme. |

## 5. Live updates

- `GET /api/v1/live` upgrades to a WebSocket; requires the session cookie and a same-origin `Origin` header; per-connection rate limits.
- The server listens on `braid_derived` (02) and pushes only events of the connection's user: `session_updated {session_id}`, `device_synced {device_id}`, `reindex {state}`. The client refetches what it shows; no data in events.
- Reconnect with backoff; a banner shows "live paused" while disconnected.

## 6. Tests

- vitest for components and data formatting (cost, durations, time zones).
- Playwright end-to-end against a server started with fixture data (from 02's golden fixtures): every view renders, filters change results, canvas shows main + subagents for a nested fixture, WebSocket update appears without reload, admin view hidden for non-admins.
- Visual check against ponytail board screenshots during review (manual).
