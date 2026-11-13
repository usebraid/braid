// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Adapted from ponytail (https://github.com/DietrichGebert/ponytail, hooks/band/board.ts @96608e0), Copyright (c) 2026 DietrichGebert,
// MIT License; the license text is in THIRD_PARTY_NOTICES.md.

// Thermal ramp: a quiet indigo for a little activity, through red to yellow for the most.
type Rgb = [number, number, number]
const HEAT: [number, Rgb][] = [
  [0, [58, 48, 120]],
  [0.4, [150, 45, 112]],
  [0.65, [235, 72, 58]],
  [0.85, [255, 140, 60]],
  [1, [255, 214, 102]],
]

export function heatColor(a: number): string {
  for (let i = 1; i < HEAT.length; i++) {
    const [t1, hi] = HEAT[i] as [number, Rgb]
    if (a <= t1) {
      const [t0, lo] = HEAT[i - 1] as [number, Rgb]
      const t = (a - t0) / (t1 - t0)
      return `rgb(${lo.map((c, j) => Math.round(c + ((hi[j] as number) - c) * t)).join(',')})`
    }
  }
  return 'rgb(255,214,102)'
}

export function hexPath(cx: number, cy: number, r: number): string {
  let d = ''
  for (let k = 0; k < 6; k++) {
    const a = (Math.PI / 3) * k - Math.PI / 2
    d += `${k ? 'L' : 'M'}${(cx + r * Math.cos(a)).toFixed(1)},${(cy + r * Math.sin(a)).toFixed(1)}`
  }
  return `${d}Z`
}
