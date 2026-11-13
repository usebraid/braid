// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { dayKey } from './format'

const MAX_DAYS = 1830 // the API allows five years

// The next calendar day of a "YYYY-MM-DD" key; plain date arithmetic, so DST in the user's zone cannot skip or repeat a day.
const next = (key: string) => new Date(Date.parse(`${key}T00:00:00Z`) + 864e5).toISOString().slice(0, 10)

// Every local day key from `first` to `last`, both included.
export function daysBetween(first: string, last: string): string[] {
  const out: string[] = []
  for (let k = first; k <= last && out.length < MAX_DAYS; k = next(k)) out.push(k)
  return out
}

// Every local day of [from, to) in timeZone, as the overview plots them: zero days included.
export function dayRange(from: string, to: string, timeZone: string): string[] {
  return daysBetween(dayKey(from, timeZone), dayKey(new Date(Date.parse(to) - 1).toISOString(), timeZone))
}

// The values of `rows` on `keys`, 0 where a day has no row.
export function fillDays<T extends { day: string }>(keys: string[], rows: T[], pick: (r: T) => number): number[] {
  const by = new Map(rows.map((r) => [r.day, pick(r)]))
  return keys.map((k) => by.get(k) ?? 0)
}

// "All" reaches back to the first day with data, and covers at least a week, as on ponytail's board.
export function fromFirst(keys: string[], first: string | undefined): string[] {
  const i = first ? keys.indexOf(first) : -1
  return keys.slice(Math.max(0, Math.min(i < 0 ? keys.length : i, keys.length - 7)))
}
