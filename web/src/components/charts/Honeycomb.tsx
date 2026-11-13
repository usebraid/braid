// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Adapted from ponytail (https://github.com/DietrichGebert/ponytail, hooks/band/board.ts @96608e0), Copyright (c) 2026 DietrichGebert,
// MIT License; the license text is in THIRD_PARTY_NOTICES.md.

import { type KeyboardEvent, useId, useState } from 'react'
import { heatColor, hexPath } from './heat'
import { Blur } from './Sparkline'

const R = 17
const CW = Math.sqrt(3) * R
const LEFT = 44
const TOP = R + 2
const LEGEND_STEP = 15
const MOVES: Record<string, number> = { ArrowUp: -1, ArrowDown: 1, ArrowLeft: -7, ArrowRight: 7 }

// The days as a honeycomb: a column per week, a row per weekday (Sunday on top), the colour is the day's value.
export function Honeycomb(p: {
  keys: string[] // consecutive "YYYY-MM-DD" days
  values: number[]
  title: string
  label: (key: string) => string
  format: (v: number) => string
}) {
  const uid = useId().replace(/:/g, '')
  const [active, setActive] = useState<number | null>(null)
  const n = p.keys.length
  const max = Math.max(0, ...p.values)
  const firstDow = p.keys[0] ? new Date(`${p.keys[0]}T00:00:00Z`).getUTCDay() : 0
  const cols = Math.ceil((n + firstDow) / 7)
  const cells = p.keys.map((k, i) => {
    const pos = i + firstDow
    const row = pos % 7
    const v = p.values[i] ?? 0
    return {
      k,
      v,
      a: max ? v / max : 0,
      cx: LEFT + Math.floor(pos / 7) * CW + (row % 2 ? CW / 2 : 0),
      cy: TOP + row * R * 1.5,
    }
  })
  const right = LEFT + cols * CW + CW / 2
  const ly = TOP + 6 * R * 1.5 + R + 20
  const end = right - 40
  const say = (i: number) => {
    const c = cells[i]
    return c ? `${p.label(c.k)}: ${c.v ? p.format(c.v) : 'no session'}` : ''
  }
  const busiest = p.values.reduce((b, v, i) => (v > (p.values[b] ?? 0) ? i : b), n - 1)
  const onKey = (e: KeyboardEvent) => {
    if (e.key === 'Escape') setActive(null)
    const m = MOVES[e.key]
    if (!m) return
    e.preventDefault()
    setActive((a) => {
      const to = (a ?? busiest) + m
      return to >= 0 && to < n ? to : a
    })
  }

  return (
    // biome-ignore lint/a11y/useSemanticElements: a keyboard-navigable chart, role=group with a label is the contract
    // biome-ignore lint/a11y/noNoninteractiveElementInteractions: the group is the single keyboard stop of the chart
    <div
      className="heat"
      role="group"
      aria-label={p.title}
      // biome-ignore lint/a11y/noNoninteractiveTabindex: one tab stop for the whole chart, arrows move inside
      tabIndex={0}
      onFocus={() => setActive((a) => a ?? busiest)}
      onBlur={() => setActive(null)}
      onKeyDown={onKey}
      onMouseLeave={() => setActive(null)}
    >
      <svg viewBox={`0 0 ${Math.ceil(right + 2)} ${Math.ceil(ly + 10)}`} aria-hidden="true">
        <defs>
          <Blur id={`${uid}-g`} sd={6} />
        </defs>
        {cells.map((c) =>
          c.a > 0.6 ? (
            <path
              key={`g${c.k}`}
              d={hexPath(c.cx, c.cy, R)}
              fill={heatColor(c.a)}
              filter={`url(#${uid}-g)`}
              className="glow-cell"
            />
          ) : null,
        )}
        {cells.map((c, i) => {
          const on = active === i
          return (
            // biome-ignore lint/a11y/noStaticElementInteractions: mouse-only hover, the keyboard path is the group's arrow keys
            <path
              key={c.k}
              d={hexPath(c.cx, c.cy, R - 2)}
              fill={c.v ? heatColor(c.a) : undefined}
              className={[c.v ? '' : 'cell-empty', on ? 'cell-on' : ''].join(' ').trim() || undefined}
              onMouseEnter={() => setActive(i)}
            >
              <title>{say(i)}</title>
            </path>
          )
        })}
        {['Mon', 'Wed', 'Fri'].map((d, i) => (
          <text key={d} x={0} y={TOP + (1 + 2 * i) * R * 1.5 + 4} className="day">
            {d}
          </text>
        ))}
        <text x={end - 4 * LEGEND_STEP - 14} y={ly + 4} textAnchor="end" className="day">
          Less
        </text>
        {[0.1, 0.3, 0.55, 0.8, 1].map((a, i) => (
          <path key={a} d={hexPath(end - (4 - i) * LEGEND_STEP, ly, 6.5)} fill={heatColor(a)} />
        ))}
        <text x={right} y={ly + 4} textAnchor="end" className="day">
          More
        </text>
      </svg>
      <div className="heat-live" aria-live="polite">
        {active !== null ? say(active) : ''}
      </div>
      {/* a div, not the table, is the 1px box: a table ignores width and height and stretched the page */}
      <div className="sr-only">
        <table>
          <caption>{p.title}</caption>
          <tbody>
            <tr>
              <th scope="col">Day</th>
              <th scope="col">Value</th>
            </tr>
            {cells
              .filter((c) => c.v > 0)
              .map((c) => (
                <tr key={c.k}>
                  <th scope="row">{p.label(c.k)}</th>
                  <td>{p.format(c.v)}</td>
                </tr>
              ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
