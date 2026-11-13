# Third-party notices

Braid is licensed under AGPL-3.0-or-later. Code from ponytail is included (the price table and cost formula, the dashboard look and its UI and chart code, the band module and the plugin manifests; see "Included files" below); only ideas, no code, were taken from agents-observe. This file holds the license texts (copied code keeps its original copyright header).

## agents-observe (https://github.com/simple10/agents-observe)

MIT License

Copyright (c) 2025 Joe Johnston

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

## ponytail (https://github.com/DietrichGebert/ponytail)

MIT License

Copyright (c) 2026 DietrichGebert

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

Included files (each adapted from `hooks/band/board.ts` @96608e0):

- `internal/server/derive/prices.go`: the price table and cost formula.
- `web/src/lib/format.ts`: `time`, `tok`, `money`, `modelName` (formatting helpers).
- `web/src/theme/tokens.css`: the `CSS` constant's dark variables, `LIGHT`, `PALETTE` and the KPI colour pairs.
- `web/src/styles/app.css`: the `CSS` constant's rules for `.card .kpi .pill .seg .btn .chip .tip .bar .legend .grid2 table .side .nav-item .brand .top h1 .num`.
- `web/src/theme/theme.ts`: `THEMES` and `applyTheme`.
- `web/src/components/ui.tsx`: the KPI, segmented control, chip and theme button markup.
- `web/src/components/charts/Sparkline.tsx`, `StreamChart.tsx`, `smooth.ts`, `Honeycomb.tsx`, `BarList.tsx`: the sparkline, stream chart, smoothing, heatmap and bar drawing code (added with the dashboard charts).
- `site/style.css`: the colour tokens and base rules, copied from `web/src/theme/tokens.css` and `web/src/styles/app.css` (themselves adapted from ponytail, see above).
- `band/hooks/band/*.ts(x)`: adapted from `hooks/band/register.tsx` and `hooks/band/types.d.ts` @96608e0 (module structure, palette names, slot stacking).
- `band/.claude-plugin/plugin.json`, `.claude-plugin/marketplace.json`: structural templates from ponytail's `.claude-plugin/plugin.json` and `.claude-plugin/marketplace.json`.

## coder/websocket (https://github.com/coder/websocket)

Used unmodified as a Go dependency (WebSocket server for `/api/v1/live`).

ISC License

Copyright (c) 2025 Coder

Permission to use, copy, modify, and distribute this software for any
purpose with or without fee is hereby granted, provided that the above
copyright notice and this permission notice appear in all copies.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.

## Build tools

`marked` (MIT, https://github.com/markedjs/marked) renders the Markdown docs of the site at build time. It is not included in any released archive, the server image or the web app bundle, so its license text is not reproduced here; the generated pages contain no marked code.

The licenses of the third-party packages bundled into the web app are listed in `/third-party-licenses.txt` of the running app.
