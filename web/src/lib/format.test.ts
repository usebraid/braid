// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { describe, expect, it } from 'vitest'
import { dayKey, fmtCost, fmtDateTime, fmtDuration, fmtLost, fmtTokens, modelName } from './format'

describe('format', () => {
  it('durations', () => {
    expect([0, 999, 45_000, 200_000, 3_599_000, 7_500_000].map(fmtDuration)).toEqual([
      '0s',
      '1s',
      '45s',
      '3m 20s',
      '59m 59s',
      '2h 5m',
    ])
  })
  it('tokens', () => {
    expect([0, 999, 1000, 1500, 1_234_567].map(fmtTokens)).toEqual(['0', '999', '1k', '1.5k', '1.2M'])
  })
  it('cost', () => {
    expect([0, 0.004, 0.0195, 12.5, 1234.567].map(fmtCost)).toEqual(['$0.00', '<$0.01', '$0.02', '$12.50', '$1,234.57'])
  })
  it('time lost', () => {
    expect([45, 600, 5400, 7200].map(fmtLost)).toEqual(['45s', '10m', '1h 30m', '2h 0m'])
  })
  it('model names', () => {
    expect(['claude-opus-5-5', 'claude-sonnet-5-5-20260101', 'claude-fable-5-2', 'weird'].map(modelName)).toEqual([
      'Opus 5.5',
      'Sonnet 5.5',
      'Fable 5.2',
      'Weird',
    ])
  })
  it('day keys follow the time zone', () => {
    const t = '2026-11-01T23:30:00Z'
    expect(dayKey(t, 'UTC')).toBe('2026-11-01')
    expect(dayKey(t, 'Europe/Berlin')).toBe('2026-11-02')
    expect(dayKey(t, 'America/Los_Angeles')).toBe('2026-11-01')
    expect(dayKey('2026-03-29T00:30:00Z', 'Europe/Berlin')).toBe('2026-03-29') // DST switch day
  })
  it('date times follow the time zone', () => {
    expect(fmtDateTime('2026-11-01T23:30:00Z', 'Europe/Berlin')).toBe('Nov 2, 2026, 00:30')
  })
})
