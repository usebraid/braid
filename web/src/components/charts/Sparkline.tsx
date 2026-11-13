// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Adapted from ponytail (https://github.com/DietrichGebert/ponytail, hooks/band/board.ts @96608e0), Copyright (c) 2026 DietrichGebert,
// MIT License; the license text is in THIRD_PARTY_NOTICES.md.

import { avg, smooth } from './smooth'

type Stop = [offset: number, color: string, opacity: number]

// userSpaceOnUse linear gradient from the origin to (x2, y2), as ponytail's `gradient` helper.
export function Gradient(p: { id: string; x2: number; y2: number; stops: Stop[] }) {
  return (
    <linearGradient id={p.id} gradientUnits="userSpaceOnUse" x1={0} y1={0} x2={p.x2} y2={p.y2}>
      {p.stops.map(([o, c, a]) => (
        <stop key={o} offset={o} style={{ stopColor: c }} stopOpacity={a} />
      ))}
    </linearGradient>
  )
}

export function Blur(p: { id: string; sd: number }) {
  return (
    <filter id={p.id} x="-20%" y="-50%" width="140%" height="200%">
      <feGaussianBlur stdDeviation={p.sd} />
    </filter>
  )
}

const W = 300
const H = 58

export function Sparkline(p: { values: number[]; colors: [string, string]; id: string }) {
  const svgProps = { viewBox: `0 0 ${W} ${H}`, preserveAspectRatio: 'none', className: 'spark', 'aria-hidden': true }
  const vals = p.values.length > 14 ? avg(p.values, 2) : p.values
  const max = Math.max(0, ...vals)
  if (max <= 0) return <svg {...svgProps} />
  const line = smooth(vals.map((v, j) => [(j * W) / Math.max(1, vals.length - 1), H - 6 - (v / max) * (H - 16)]))
  const [c1, c2] = p.colors
  const stroke = `url(#${p.id}-l)`
  return (
    // biome-ignore lint/a11y/noSvgWithoutTitle: decorative, the KPI card carries the text
    <svg {...svgProps}>
      <defs>
        <Gradient
          id={`${p.id}-l`}
          x2={W}
          y2={0}
          stops={[
            [0, c1, 1],
            [1, c2, 1],
          ]}
        />
        <Gradient
          id={`${p.id}-f`}
          x2={0}
          y2={H}
          stops={[
            [0, c2, 0.38],
            [1, c1, 0],
          ]}
        />
        <Blur id={`${p.id}-g`} sd={5} />
      </defs>
      <path d={`${line}L${W},${H}L0,${H}Z`} fill={`url(#${p.id}-f)`} />
      <path
        d={line}
        fill="none"
        stroke={stroke}
        strokeWidth={2}
        filter={`url(#${p.id}-g)`}
        className="glow-line"
        vectorEffect="non-scaling-stroke"
      />
      <path d={line} fill="none" stroke={stroke} strokeWidth={1.8} vectorEffect="non-scaling-stroke" />
    </svg>
  )
}
