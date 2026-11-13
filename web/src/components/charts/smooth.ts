// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Adapted from ponytail (https://github.com/DietrichGebert/ponytail, hooks/band/board.ts @96608e0), Copyright (c) 2026 DietrichGebert,
// MIT License; the license text is in THIRD_PARTY_NOTICES.md.

export type Pt = [number, number]

export const avg = (values: number[], r: number): number[] =>
  values.map((_, i) => {
    const a = values.slice(Math.max(0, i - r), i + r + 1)
    return a.reduce((s, v) => s + v, 0) / a.length
  })

const f = (n: number) => n.toFixed(1)

// Monotone cubic through the points (as d3's curveMonotoneX): flat at peaks and valleys, never past a point.
export function smooth(p: Pt[]): string {
  const at = (i: number): Pt => p[i] as Pt
  const n = p.length
  if (n === 0) return ''
  let d = `M${f(at(0)[0])},${f(at(0)[1])}`
  if (n < 3) return n < 2 ? d : `${d}L${f(at(1)[0])},${f(at(1)[1])}`
  const slope = (i: number) => (at(i + 1)[1] - at(i)[1]) / (at(i + 1)[0] - at(i)[0])
  const m: number[] = []
  for (let i = 1; i < n - 1; i++) {
    const s0 = slope(i - 1)
    const s1 = slope(i)
    m[i] = (Math.sign(s0) + Math.sign(s1)) * Math.min(Math.abs(s0), Math.abs(s1), Math.abs(s0 + s1) / 4) || 0
  }
  m[0] = (3 * slope(0) - (m[1] as number)) / 2
  m[n - 1] = (3 * slope(n - 2) - (m[n - 2] as number)) / 2
  for (let i = 0; i < n - 1; i++) {
    const dx = (at(i + 1)[0] - at(i)[0]) / 3
    d += `C${f(at(i)[0] + dx)},${f(at(i)[1] + (m[i] as number) * dx)} ${f(at(i + 1)[0] - dx)},${f(at(i + 1)[1] - (m[i + 1] as number) * dx)} ${f(at(i + 1)[0])},${f(at(i + 1)[1])}`
  }
  return d
}
