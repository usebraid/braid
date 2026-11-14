// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

export type LaneAgent = { id: string; parent: string | null; from: string | null; to: string | null }

export const MAX_LANES = 6

const ms = (iso: string | null, none: number) => {
  const v = iso ? Date.parse(iso) : Number.NaN
  return Number.isNaN(v) ? none : v
}

// Lane 0 is main; an agent takes the first lane right of its parent whose occupant merged before it started.
// Times compare as instants: API timestamps differ in their fractional digits, so text order is wrong.
export function assignLanes(agents: LaneAgent[]): Map<string, number> {
  const lanes = new Map<string, number>([['main', 0]])
  const busyUntil: number[] = []
  const from = (a: LaneAgent) => ms(a.from, Number.NEGATIVE_INFINITY)
  const sorted = agents.filter((a) => a.id !== 'main').sort((a, b) => from(a) - from(b) || 0)
  for (const a of sorted) {
    let lane = (a.parent === null ? 0 : (lanes.get(a.parent) ?? 0)) + 1
    while (lane < MAX_LANES - 1 && (busyUntil[lane] ?? Number.NEGATIVE_INFINITY) > from(a)) lane++
    lane = Math.min(lane, MAX_LANES - 1)
    busyUntil[lane] = Math.max(busyUntil[lane] ?? Number.NEGATIVE_INFINITY, ms(a.to, Number.POSITIVE_INFINITY)) // open agents never free their lane
    lanes.set(a.id, lane)
  }
  return lanes
}
