// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Adapted from ponytail (https://github.com/DietrichGebert/ponytail, hooks/band/board.ts @96608e0), Copyright (c) 2026 DietrichGebert,
// MIT License; the license text is in THIRD_PARTY_NOTICES.md.

import { type KeyboardEvent, useId, useRef, useState } from 'react'
import { Blur, Gradient } from './Sparkline'
import { type Pt, smooth } from './smooth'

export type SeriesPoint = { key: string; label: string; value: number } // key "2026-11-02", label "Nov 2"
// Optional stacking: one layer per project, values aligned with `points` (bottom layer first).
export type Layer = { key: string; label: string; color: string; values: number[] }

const W = 900
const H = 340
const TOP = 66
const BASE = 282

export function StreamChart(p: {
  points: SeriesPoint[]
  format: (v: number) => string
  title: string
  colors?: [string, string]
  empty?: string
  layers?: Layer[]
}) {
  const uid = useId().replace(/:/g, '')
  const [active, setActive] = useState<number | null>(null)
  const cols = useRef<(SVGRectElement | null)[]>([])
  const n = p.points.length
  const totals = p.points.map((q) => q.value)
  const sum = totals.reduce((a, v) => a + v, 0)
  const max = Math.max(0, ...totals)
  if (n === 0 || max <= 0) return <div className="empty-note">{p.empty ?? 'No data in this period'}</div>
  // one point is no trend: an area from it would fill the chart with a single slope
  if (n < 2)
    return (
      <div className="empty-note" role="img" aria-label={`${p.title}: total ${p.format(sum)}, 1 day`}>
        Not enough days for a trend yet
      </div>
    )

  const layers: Layer[] = p.layers ?? [
    { key: 'all', label: p.title, color: (p.colors ?? ['var(--p1)', 'var(--p2)'])[0], values: totals },
  ]
  const x = (i: number) => (n > 1 ? (i * W) / (n - 1) : W / 2)
  const y = (v: number) => BASE - (v / max) * (BASE - TOP)
  let acc = totals.map(() => 0)
  const shapes = layers.map((l, li) => {
    const lower = acc
    const upper = acc.map((a, i) => a + (l.values[i] ?? 0))
    acc = upper
    const up = smooth(upper.map((v, i): Pt => [x(i), y(v)]))
    const low = smooth(lower.map((v, i): Pt => [x(i), y(v)]).reverse())
    return { l, li, up, d: `${up}L${low.slice(1)}Z` }
  })
  const top = shapes[shapes.length - 1] as (typeof shapes)[number]
  const step = Math.max(1, Math.ceil(n / 6))
  const ticks = Array.from({ length: Math.ceil(n / step) }, (_, k) => k * step)
  const cur = active === null ? null : p.points[active]
  const move = (e: KeyboardEvent, i: number) => {
    if (e.key === 'Escape') setActive(null)
    else if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
      e.preventDefault()
      cols.current[Math.max(0, Math.min(n - 1, i + (e.key === 'ArrowRight' ? 1 : -1)))]?.focus()
    }
  }

  return (
    // biome-ignore lint/a11y/noNoninteractiveElementInteractions: mouse-only convenience to clear the hover, keyboard has Escape
    <figure className="chart-wrap" onMouseLeave={() => setActive(null)}>
      {/* biome-ignore lint/a11y/noSvgWithoutTitle: the labelled <g role="img"> inside names the picture; a <title> here would add a hover tooltip over the whole chart */}
      <svg className="stream" viewBox={`0 0 ${W} ${H}`}>
        {/* the picture is the img; the focusable columns sit beside it, since an img may not contain interactive children */}
        {/* biome-ignore lint/a11y/noInteractiveElementToNoninteractiveRole: a <g> is not interactive, biome mistakes it for the SVG anchor-like set */}
        <g role="img" aria-label={`${p.title}: total ${p.format(sum)}, ${n} days`}>
          <defs>
            <Blur id={`${uid}-glow`} sd={10} />
            {shapes.map((s) => (
              <Gradient
                key={s.l.key}
                id={`${uid}-f${s.li}`}
                x2={0}
                y2={BASE}
                stops={[
                  [0, s.l.color, 0.95],
                  [1, s.l.color, 0.55],
                ]}
              />
            ))}
          </defs>
          <line x1={0} x2={W} y1={BASE + 22} y2={BASE + 22} className="base" />
          {ticks.map((i) => (
            <text key={i} x={x(i)} y={H - 6} className="axis" textAnchor={i ? 'middle' : 'start'}>
              {p.points[i]?.label}
            </text>
          ))}
          <path
            d={`${top.up}L${W},${BASE}L0,${BASE}Z`}
            fill={top.l.color}
            filter={`url(#${uid}-glow)`}
            className="glow"
          />
          {shapes.map((s) => (
            <path key={s.l.key} d={s.d} fill={`url(#${uid}-f${s.li})`} />
          ))}
          <path d={top.up} className="top-line" strokeWidth={1.2} />
          {active !== null ? (
            <line x1={x(active)} x2={x(active)} y1={TOP - 12} y2={BASE} className="hover-line" />
          ) : null}
        </g>
        {p.points.map((q, i) => (
          // biome-ignore lint/a11y/useSemanticElements: an SVG rect cannot be a <button>; role+tabindex+key handler is the contract
          <rect
            key={q.key}
            ref={(el) => {
              cols.current[i] = el
            }}
            x={n > 1 ? x(i) - W / (n - 1) / 2 : 0}
            y={0}
            width={n > 1 ? W / (n - 1) : W}
            height={BASE + 22}
            fill="transparent"
            tabIndex={0}
            role="button"
            aria-label={`${q.label}: ${p.format(q.value)}`}
            onFocus={() => setActive(i)}
            onBlur={() => setActive(null)}
            onMouseEnter={() => setActive(i)}
            onKeyDown={(e) => move(e, i)}
          />
        ))}
      </svg>
      <div
        role="status"
        className={cur ? 'tip' : undefined}
        style={
          cur && active !== null
            ? { left: `${(x(active) / W) * 100}%`, top: `${(y(totals[active] ?? 0) / H) * 100}%` }
            : undefined
        }
      >
        {cur && active !== null ? (
          <>
            <div className="tip-date">{cur.label}</div>
            <div className="tip-value">{p.format(cur.value)}</div>
            {p.layers
              ? [...p.layers]
                  .reverse()
                  .filter((l) => (l.values[active] ?? 0) > 0)
                  .map((l) => (
                    <div key={l.key} className="tip-row">
                      <span className="dot" style={{ background: l.color }} /> {l.label}{' '}
                      {p.format(l.values[active] ?? 0)}
                    </div>
                  ))
              : null}
          </>
        ) : null}
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
            {p.points.map((q) => (
              <tr key={q.key}>
                <th scope="row">{q.label}</th>
                <td>{p.format(q.value)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </figure>
  )
}
