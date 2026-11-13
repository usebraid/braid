// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

export type RangeId = '7d' | '30d' | '90d' | 'all'
export type Filters = {
  range?: RangeId
  from?: string
  to?: string
  device?: string
  project?: string
  model?: string
  skill?: string
}

export const RANGES: { id: RangeId; label: string; days: number }[] = [
  { id: '7d', label: '7 days', days: 7 },
  { id: '30d', label: '30 days', days: 30 },
  { id: '90d', label: '90 days', days: 90 },
  // the API allows at most 5 years
  { id: 'all', label: 'All time', days: 1825 - 1 },
]

const TEXT_KEYS = ['from', 'to', 'device', 'project', 'model', 'skill'] as const

export function parseFilters(sp: URLSearchParams): Filters {
  const f: Filters = {}
  const range = RANGES.find((r) => r.id === sp.get('range'))
  if (range) f.range = range.id
  for (const k of TEXT_KEYS) {
    const v = sp.get(k)
    if (v) f[k] = v
  }
  return f
}

export function withFilter(sp: URLSearchParams, key: keyof Filters, value: string): URLSearchParams {
  const next = new URLSearchParams(sp)
  if (key === 'range') {
    next.delete('from')
    next.delete('to')
  }
  if (value === '') next.delete(key)
  else next.set(key, value)
  return next
}

export function toQuery(f: Filters, now: Date = new Date()): Record<string, string> {
  const q: Record<string, string> = {}
  if (f.from && f.to) {
    q.from = f.from
    q.to = f.to
  } else {
    const days = RANGES.find((r) => r.id === (f.range ?? '30d'))?.days ?? 30
    q.from = new Date(now.getTime() - days * 864e5).toISOString()
    q.to = now.toISOString()
  }
  for (const k of ['device', 'project', 'model', 'skill'] as const) {
    const v = f[k]
    if (v) q[k] = v
  }
  return q
}
