// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { expect, it } from 'vitest'
import { dayRange, daysBetween, fillDays, fromFirst } from './days'

it('daysBetween lists every day, across month and year ends', () => {
  expect(daysBetween('2026-12-30', '2027-01-02')).toEqual(['2026-12-30', '2026-12-31', '2027-01-01', '2027-01-02'])
  expect(daysBetween('2026-11-02', '2026-11-02')).toEqual(['2026-11-02'])
  expect(daysBetween('2026-11-03', '2026-11-02')).toEqual([])
})

it('dayRange covers [from, to) in the user time zone, DST included', () => {
  // 30 days ending at midnight UTC; in Berlin the range starts at 01:00 on Oct 3 and ends 01:00 on Nov 2
  const berlin = dayRange('2026-10-03T00:00:00Z', '2026-11-02T00:00:00Z', 'Europe/Berlin')
  expect(berlin[0]).toBe('2026-10-03')
  expect(berlin.at(-1)).toBe('2026-11-02')
  expect(berlin).toHaveLength(31) // the DST change on Oct 25 neither skips nor repeats a day
  expect(new Set(berlin).size).toBe(31)
  expect(dayRange('2026-10-03T00:00:00Z', '2026-11-02T00:00:00Z', 'UTC')).toHaveLength(30)
  expect(dayRange('2026-11-01T22:30:00Z', '2026-11-02T00:30:00Z', 'America/Los_Angeles')).toEqual(['2026-11-01'])
})

it('fillDays puts zero on days without a row', () => {
  const rows = [
    { day: '2026-11-02', v: 5 },
    { day: '2026-11-04', v: 7 },
    { day: '2026-12-01', v: 9 }, // outside the keys
  ]
  expect(fillDays(['2026-11-01', '2026-11-02', '2026-11-03', '2026-11-04'], rows, (r) => r.v)).toEqual([0, 5, 0, 7])
})

it('fromFirst starts at the first day with data and keeps at least a week', () => {
  const keys = daysBetween('2026-11-01', '2026-11-30')
  expect(fromFirst(keys, '2026-11-10')[0]).toBe('2026-11-10')
  expect(fromFirst(keys, '2026-11-28')).toHaveLength(7) // a week back from the end
  expect(fromFirst(keys, undefined)).toHaveLength(7)
  expect(fromFirst(keys, '2026-10-01')).toHaveLength(7) // not in range
  expect(fromFirst(keys.slice(0, 3), '2026-11-02')).toHaveLength(3)
})
