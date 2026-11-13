// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Adapted from ponytail (https://github.com/DietrichGebert/ponytail, hooks/band/board.ts @96608e0), Copyright (c) 2026 DietrichGebert,
// MIT License; the license text is in THIRD_PARTY_NOTICES.md.

import { type KeyboardEvent, type MouseEvent, useId, useLayoutEffect, useRef, useState } from 'react'
import { Blur, Gradient } from './Sparkline'
import { type Pt, smooth } from './smooth'

// One point per day of the range, zero days included: key "2026-11-02", label "Nov 2".
export type SeriesPoint = { key: string; label: string; value: number }
// Optional stacking: one layer per project, values aligned with `points` (bottom layer first).
export type Layer = { key: string; label: string; color: string; values: number[] }
// One dot per session under the axis, on its day.
export type Dot = { i: number; color: string }

// The chart draws at its real size, as ponytail does, so strokes and text are never stretched.
function useSize<T extends Element>(): [React.RefObject<T | null>, { w: number; h: number }] {
  const ref = useRef<T>(null)
  const [size, setSize] = useState({ w: 900, h: 340 })
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const read = () => {
      const r = el.getBoundingClientRect()
      if (r.width > 0 && r.height > 0) setSize({ w: Math.max(320, Math.round(r.width)), h: Math.round(r.height) })
    }
    read()
    const ro = new ResizeObserver(read)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  return [ref, size]
}

export function StreamChart(p: {
  points: SeriesPoint[]
  format: (v: number) => string
  title: string
  colors?: [string, string]
  empty?: string
  layers?: Layer[]
  dots?: Dot[]
}) {
  const uid = useId().replace(/:/g, '')
  const [ref, { w: W, h: H }] = useSize<SVGSVGElement>()
  const [active, setActive] = useState<number | null>(null)
  const n = p.points.length
  const totals = p.points.map((q) => q.value)
  const sum = totals.reduce((a, v) => a + v, 0)
  const max = Math.max(0, ...totals)
  const top = Math.min(66, H * 0.19)
  const base = H - 58
  const x = (i: number) => (n > 1 ? (i * W) / (n - 1) : W / 2)
  const y = (v: number) => base - (max > 0 ? (v / max) * (base - top) : 0)
  const step = Math.max(1, Math.ceil(n / Math.min(6, Math.floor(W / 110)))) // ponytail's six ticks, fewer when narrow
  const ticks = Array.from({ length: Math.ceil(n / step) }, (_, k) => k * step)

  const layers: Layer[] = p.layers ?? [
    { key: 'all', label: p.title, color: (p.colors ?? ['var(--p1)', 'var(--p2)'])[0], values: totals },
  ]
  let acc = totals.map(() => 0)
  const shapes = layers.map((l, li) => {
    const lower = acc
    const upper = acc.map((a, i) => a + (l.values[i] ?? 0))
    acc = upper
    const up = smooth(upper.map((v, i): Pt => [x(i), y(v)]))
    const low = smooth(lower.map((v, i): Pt => [x(i), y(v)]).reverse())
    return { l, li, up, d: `${up}L${low.slice(1)}Z` }
  })
  const last = shapes[shapes.length - 1]
  // several sessions on one day sit side by side within the day's width
  const perDay = new Map<number, number>()
  const dots = (p.dots ?? []).map((d) => {
    const j = perDay.get(d.i) ?? 0
    perDay.set(d.i, j + 1)
    return { ...d, j }
  })
  const dotX = (i: number, j: number) => {
    const k = perDay.get(i) ?? 1
    return Math.min(W - 4, Math.max(4, x(i) + ((j + 1) / (k + 1) - 0.5) * (W / Math.max(1, n))))
  }

  const has = n > 0 && max > 0
  const cur = has && active !== null ? p.points[active] : undefined
  const clamp = (i: number) => Math.max(0, Math.min(n - 1, i))
  const onKey = (e: KeyboardEvent) => {
    if (e.key === 'Escape') setActive(null)
    else if (e.key === 'ArrowRight' || e.key === 'ArrowLeft' || e.key === 'Home' || e.key === 'End') {
      e.preventDefault()
      const d = { ArrowRight: 1, ArrowLeft: -1, Home: -n, End: n }[e.key]
      setActive((a) => clamp((a ?? (d > 0 ? -1 : n)) + d))
    }
  }
  const onMove = (e: MouseEvent) => {
    const r = ref.current?.getBoundingClientRect()
    if (r && r.width > 0) setActive(clamp(Math.round(((e.clientX - r.left) / r.width) * (n - 1))))
  }

  return (
    // biome-ignore lint/a11y/useSemanticElements: a keyboard-navigable chart, role=group with a label is the contract
    // biome-ignore lint/a11y/noNoninteractiveElementInteractions: the group is the single keyboard stop of the chart
    <div
      className="chart-wrap"
      role="group"
      aria-label={`${p.title}, use the arrow keys to read each day`}
      tabIndex={has ? 0 : undefined}
      onKeyDown={has ? onKey : undefined}
      onFocus={() => setActive((a) => a ?? (has ? n - 1 : null))}
      onBlur={() => setActive(null)}
      onMouseMove={has ? onMove : undefined}
      onMouseLeave={() => setActive(null)}
    >
      {/* biome-ignore lint/a11y/noSvgWithoutTitle: the labelled <g role="img"> inside names the picture */}
      <svg ref={ref} className="stream" viewBox={`0 0 ${W} ${H}`}>
        {/* biome-ignore lint/a11y/noInteractiveElementToNoninteractiveRole: a <g> is not interactive, biome mistakes it for the SVG anchor-like set */}
        <g role="img" aria-label={has ? `${p.title}: total ${p.format(sum)}, ${n} days` : p.title}>
          <defs>
            <Blur id={`${uid}-glow`} sd={10} />
            {shapes.map((s) => (
              <Gradient
                key={s.l.key}
                id={`${uid}-f${s.li}`}
                x2={0}
                y2={base}
                stops={[
                  [0, s.l.color, 0.95],
                  [1, s.l.color, 0.55],
                ]}
              />
            ))}
          </defs>
          <line x1={0} x2={W} y1={base + 22} y2={base + 22} className="base" />
          {ticks.map((i) => (
            <text key={i} x={x(i)} y={H - 6} className="axis" textAnchor={i ? 'middle' : 'start'}>
              {p.points[i]?.label}
            </text>
          ))}
          {has && last ? (
            <>
              <path
                d={`${last.up}L${W},${base}L0,${base}Z`}
                style={{ fill: 'var(--p1)' }}
                filter={`url(#${uid}-glow)`}
                className="glow"
              />
              {shapes.map((s) => (
                <path key={s.l.key} d={s.d} fill={`url(#${uid}-f${s.li})`} />
              ))}
              <path d={last.up} className="top-line" strokeWidth={1.2} />
              {dots.map((d) => (
                <circle key={`${d.i}-${d.j}`} cx={dotX(d.i, d.j)} cy={base + 22} r={3.2} style={{ fill: d.color }} />
              ))}
            </>
          ) : (
            <text x={W / 2} y={(top + base) / 2} textAnchor="middle" className="empty-chart">
              {p.empty ?? 'No sessions in this period'}
            </text>
          )}
          {cur && active !== null ? (
            <line x1={x(active)} x2={x(active)} y1={top - 12} y2={base} className="hover-line" />
          ) : null}
        </g>
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
      {has ? (
        <div className="sr-only">
          <table>
            <caption>{p.title}</caption>
            <tbody>
              <tr>
                <th scope="col">Day</th>
                <th scope="col">Value</th>
              </tr>
              {p.points
                .filter((q) => q.value > 0)
                .map((q) => (
                  <tr key={q.key}>
                    <th scope="row">{q.label}</th>
                    <td>{p.format(q.value)}</td>
                  </tr>
                ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </div>
  )
}
