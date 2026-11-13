// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

export type LaneAgent = { id: string; parent: string | null; from: string | null; to: string | null }

export const MAX_LANES = 6

const END = '￿' // open agents never free their lane

// Lane 0 is main; an agent takes the first lane right of its parent whose occupant merged before it started.
export function assignLanes(agents: LaneAgent[]): Map<string, number> {
  const lanes = new Map<string, number>([['main', 0]])
  const busyUntil: string[] = []
  const sorted = agents.filter((a) => a.id !== 'main').sort((a, b) => (a.from ?? '').localeCompare(b.from ?? ''))
  for (const a of sorted) {
    const from = a.from ?? ''
    let lane = (a.parent === null ? 0 : (lanes.get(a.parent) ?? 0)) + 1
    while (lane < MAX_LANES - 1 && (busyUntil[lane] ?? '') > from) lane++
    lane = Math.min(lane, MAX_LANES - 1)
    busyUntil[lane] = [busyUntil[lane] ?? '', a.to ?? END].sort().at(-1) ?? END
    lanes.set(a.id, lane)
  }
  return lanes
}
