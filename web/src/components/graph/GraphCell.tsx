// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import type { CSSProperties } from 'react'
import type { Row } from '../../lib/chatRows'
import { MAX_LANES } from '../../lib/lanes'
import type { Entry } from '../../lib/transcript'

const STEP = 14
const PAD = 10
const DOT_Y = 14
const GRAPH_W = PAD * 2 + (MAX_LANES - 1) * STEP
const COLORS = ['--p1', '--p4', '--p2', '--p3', '--p5', '--p7']
const x = (lane: number) => PAD + lane * STEP
const color = (lane: number) => `var(${COLORS[lane % COLORS.length]})`
const curve = (x0: number, y0: number, x1: number, y1: number) =>
  `M${x0} ${y0} C ${x0} ${(y0 + y1) / 2 + 4}, ${x1} ${(y0 + y1) / 2 - 4}, ${x1} ${y1}`

// Decoration only: every row reads without it, so it is hidden from assistive tech.
export function GraphCell(p: { row: Row; through: number[]; last: boolean; pulse: boolean }) {
  const { row } = p
  const stroke = (lane: number): CSSProperties => ({ stroke: color(lane) })
  const lanes = new Set(p.through)
  lanes.add(0)
  const dotLane = row.kind === 'entry' ? row.lane : row.kind === 'branch' ? row.from : row.into
  const kind: Entry['kind'] | 'branch' | 'merge' = row.kind === 'entry' ? row.entry.kind : row.kind
  const big = kind === 'prompt'
  const filled = kind === 'prompt' || kind === 'reply' || kind === 'branch' || kind === 'merge'
  const dotColor = kind === 'prompt' ? 'var(--focus)' : color(row.lane)
  const r = big ? 5 : filled ? 4 : 3
  return (
    <svg className="gcell" width={GRAPH_W} aria-hidden="true" focusable="false">
      {[...lanes].map((l) => (
        <line key={l} x1={x(l)} x2={x(l)} y1={0} y2={l === 0 && p.last ? DOT_Y : '100%'} style={stroke(l)} />
      ))}
      {row.kind === 'branch' ? (
        <>
          <path d={curve(x(row.from), DOT_Y, x(row.lane), DOT_Y + 14)} fill="none" style={stroke(row.lane)} />
          <line x1={x(row.lane)} x2={x(row.lane)} y1={DOT_Y + 14} y2="100%" style={stroke(row.lane)} />
        </>
      ) : null}
      {row.kind === 'merge' ? (
        <path d={curve(x(row.lane), 0, x(row.into), DOT_Y)} fill="none" style={stroke(row.lane)} />
      ) : null}
      <circle
        className={p.pulse ? 'gdot pulse' : 'gdot'}
        cx={x(dotLane)}
        cy={DOT_Y}
        r={r}
        style={filled ? { fill: dotColor, stroke: dotColor } : { fill: 'var(--card-solid)', stroke: dotColor }}
      />
    </svg>
  )
}
