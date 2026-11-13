# Credits

Braid builds on ideas from these MIT-licensed projects. Code from ponytail is included (the price table and cost formula, the dashboard look and its UI and chart code); see `THIRD_PARTY_NOTICES.md`.

- [simple10/agents-observe](https://github.com/simple10/agents-observe) (Joe Johnston): reference for transcript parsing, the event model, hierarchy and replay UI ideas, and the original idea of syncing sessions.
- [DietrichGebert/ponytail](https://github.com/DietrichGebert/ponytail) (Dietrich Gebert): board look and feel and SVG charts, cost/metric logic and price table, band mod as a template, website as a model for usebraid.dev.
- agents-observe, session canvas side panel: event rows with collapsed tool calls (`app/client/src/components/event-stream`) and per-agent timeline lanes (`components/timeline`). Ideas only, no code taken.
- `site/style.css`: the site look is adapted from the dashboard CSS (`web/src/theme/tokens.css`), which is adapted from ponytail.
- [markedjs/marked](https://github.com/markedjs/marked) (MIT): renders the docs pages at build time; not part of the shipped binaries or the web app bundle.
