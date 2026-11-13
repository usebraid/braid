# Braid

Braid is a self-hosted daemon and server that syncs Claude Code transcripts from your machines into one place, so sessions, subagents, costs and failures can be browsed and replayed.

## Status

Pre-release, not usable yet.

## AGPL section 13 notice

Braid is licensed under AGPL-3.0-or-later. If you run a modified version as a network service, you must offer its users the corresponding source. The web app footer links to the source of the running version.

## Special thanks

- [ponytail](https://github.com/DietrichGebert/ponytail) by Dietrich Gebert: board look and feel and SVG charts, cost/metric logic and price table, the band mod as a template, and the website as a model for usebraid.dev.
- [agents-observe](https://github.com/simple10/agents-observe) by Joe Johnston: reference for transcript parsing, the event model, hierarchy and replay UI ideas, and the original idea of syncing sessions.

Notable dependencies: React Flow (`@xyflow/react`, MIT) and dagre (`@dagrejs/dagre`, MIT) draw the session canvas.

See `CREDITS.md` and `THIRD_PARTY_NOTICES.md`.

## License

AGPL-3.0-or-later, see `LICENSE`.
