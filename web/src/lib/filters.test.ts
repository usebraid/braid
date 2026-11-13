// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { describe, expect, it } from 'vitest'
import { parseFilters, RANGES, type RangeId, toQuery, withFilter } from './filters'

const now = new Date('2026-11-13T12:00:00Z')

describe('filters', () => {
  it('parses the URL', () => {
    expect(parseFilters(new URLSearchParams('range=7d&device=d1&project=%2Fwork%2Falpha&junk=1'))).toEqual({
      range: '7d',
      device: 'd1',
      project: '/work/alpha',
    })
    expect(parseFilters(new URLSearchParams('range=bogus'))).toEqual({})
  })
  it('turns a preset into from/to and defaults to 30 days', () => {
    expect(toQuery({ range: '7d' }, now)).toEqual({ from: '2026-11-06T12:00:00.000Z', to: '2026-11-13T12:00:00.000Z' })
    expect(toQuery({}, now).from).toBe('2026-10-14T12:00:00.000Z')
    expect(Date.parse(toQuery({ range: 'all' }, now).from ?? '')).toBeGreaterThan(now.getTime() - 5 * 365.25 * 864e5)
  })
  it('explicit from/to win and extra filters pass through', () => {
    expect(
      toQuery({ range: '7d', from: '2026-10-01T00:00:00Z', to: '2030-01-01T00:00:00Z', skill: 'x:y' }, now),
    ).toEqual({
      from: '2026-10-01T00:00:00Z',
      to: '2030-01-01T00:00:00Z',
      skill: 'x:y',
    })
  })
  it('edits search params', () => {
    const sp = withFilter(new URLSearchParams('from=a&to=b&model=m'), 'range', '90d')
    expect(sp.toString()).toBe('model=m&range=90d')
    expect(withFilter(sp, 'model', '').toString()).toBe('range=90d')
  })
  it('keeps the longest preset within the 5 year API limit', () => {
    const all: RangeId = 'all'
    expect(RANGES.find((r) => r.id === all)?.days).toBeLessThan(5 * 365)
  })
})
