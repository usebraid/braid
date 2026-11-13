// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Adapted from ponytail (https://github.com/DietrichGebert/ponytail, hooks/band/board.ts @96608e0), Copyright (c) 2026 DietrichGebert,
// MIT License; the license text is in THIRD_PARTY_NOTICES.md.

import { type KeyboardEvent, useId, useState } from 'react'
import { heatColor, hexPath } from './heat'
import { Blur } from './Sparkline'

const DAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']
const R = 17
const CW = Math.sqrt(3) * R
const LEFT = 44
const TOP = R + 2
const COLS = 24
const GRID_W = LEFT + COLS * CW + CW / 2
const GRID_BOTTOM = TOP + 7 * R * 1.5
const LEGEND_Y = GRID_BOTTOM + R + 16

type Cell = { weekday: number; hour: number; count: number }
const LEGEND_STEP = 15

export function Honeycomb(p: { cells: Cell[]; title: string; unit?: string }) {
  const uid = useId().replace(/:/g, '')
  const unit = p.unit ?? 'events'
  const counts = new Map<number, number>()
  for (const c of p.cells) counts.set(c.weekday * COLS + c.hour, c.count)
  const max = Math.max(0, ...counts.values())
  const busiest = [...p.cells].sort((a, b) => b.count - a.count)[0]
  const [active, setActive] = useState<[number, number] | null>(null)
  const focus = () => setActive((a) => a ?? [busiest?.weekday ?? 0, busiest?.hour ?? 0])
  const say = (row: number, col: number) => {
    const n = counts.get(row * COLS + col) ?? 0
    return `${DAYS[row]} ${col}:00, ${n ? `${n} ${unit}` : 'no activity'}`
  }
  const onKey = (e: KeyboardEvent) => {
    const d: Record<string, [number, number]> = {
      ArrowUp: [-1, 0],
      ArrowDown: [1, 0],
      ArrowLeft: [0, -1],
      ArrowRight: [0, 1],
    }
    const m = d[e.key]
    if (e.key === 'Escape') setActive(null)
    if (!m) return
    e.preventDefault()
    setActive((a) => {
      const [r, c] = a ?? [0, 0]
      return [Math.max(0, Math.min(6, r + m[0])), Math.max(0, Math.min(COLS - 1, c + m[1]))]
    })
  }
  const pos = (row: number, col: number) => ({ cx: LEFT + col * CW + (row % 2 ? CW / 2 : 0), cy: TOP + row * R * 1.5 })
  const rows = Array.from({ length: 7 }, (_, r) => r)
  const cols = Array.from({ length: COLS }, (_, c) => c)
  const end = GRID_W - 42

  return (
    // biome-ignore lint/a11y/useSemanticElements: a keyboard-navigable chart, role=group with a label is the contract
    // biome-ignore lint/a11y/noNoninteractiveElementInteractions: the group is the single keyboard stop of the chart
    <div
      className="heat"
      role="group"
      aria-label={p.title}
      // biome-ignore lint/a11y/noNoninteractiveTabindex: one tab stop for the whole chart, arrows move inside
      tabIndex={0}
      onFocus={focus}
      onBlur={() => setActive(null)}
      onKeyDown={onKey}
      onMouseLeave={() => setActive(null)}
    >
      <svg viewBox={`0 0 ${Math.ceil(GRID_W + 2)} ${Math.ceil(LEGEND_Y + 10)}`} aria-hidden="true">
        <defs>
          <Blur id={`${uid}-g`} sd={6} />
        </defs>
        {rows.flatMap((r) =>
          cols.map((c) => {
            const n = counts.get(r * COLS + c) ?? 0
            return n / max > 0.6 ? (
              <path
                key={`g${r}-${c}`}
                d={hexPath(pos(r, c).cx, pos(r, c).cy, R)}
                fill={heatColor(n / max)}
                filter={`url(#${uid}-g)`}
                className="glow-cell"
              />
            ) : null
          }),
        )}
        {rows.flatMap((r) =>
          cols.map((c) => {
            const n = counts.get(r * COLS + c) ?? 0
            const on = active?.[0] === r && active[1] === c
            return (
              // biome-ignore lint/a11y/noStaticElementInteractions: mouse-only hover, the keyboard path is the group's arrow keys
              <path
                key={`${r}-${c}`}
                d={hexPath(pos(r, c).cx, pos(r, c).cy, R - 2)}
                fill={n ? heatColor(n / max) : undefined}
                className={n ? (on ? 'cell-on' : undefined) : on ? 'cell-empty cell-on' : 'cell-empty'}
                onMouseEnter={() => setActive([r, c])}
              >
                <title>{say(r, c)}</title>
              </path>
            )
          }),
        )}
        {rows.map((r) => (
          <text key={r} x={0} y={pos(r, 0).cy + 4} className="day">
            {(DAYS[r] as string).slice(0, 3)}
          </text>
        ))}
        {[0, 6, 12, 18].map((h) => (
          <text key={h} x={pos(0, h).cx} y={GRID_BOTTOM + 6} className="day" textAnchor="middle">
            {h}
          </text>
        ))}
        <text x={end - 4 * LEGEND_STEP - 14} y={LEGEND_Y + 4} textAnchor="end" className="day">
          Less
        </text>
        {[0.1, 0.3, 0.55, 0.8, 1].map((a, i) => (
          <path key={a} d={hexPath(end - (4 - i) * LEGEND_STEP, LEGEND_Y, 6.5)} fill={heatColor(a)} />
        ))}
        <text x={GRID_W} y={LEGEND_Y + 4} textAnchor="end" className="day">
          More
        </text>
      </svg>
      <div className="heat-live" aria-live="polite">
        {active ? say(active[0], active[1]) : ''}
      </div>
      <table className="sr-only">
        <caption>{p.title}</caption>
        <tbody>
          <tr>
            <th scope="col">Day</th>
            <th scope="col">Hour</th>
            <th scope="col">Count</th>
          </tr>
          {[...p.cells]
            .filter((c) => c.count > 0)
            .sort((a, b) => a.weekday - b.weekday || a.hour - b.hour)
            .map((c) => (
              <tr key={`${c.weekday}-${c.hour}`}>
                <th scope="row">{DAYS[c.weekday]}</th>
                <td>{c.hour}:00</td>
                <td>{c.count}</td>
              </tr>
            ))}
        </tbody>
      </table>
    </div>
  )
}
