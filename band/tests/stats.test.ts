// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors
import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import Ajv2020 from 'ajv/dist/2020.js'
import addFormats from 'ajv-formats'
import { describe, expect, test } from 'vitest'
import { countdown, dotColor, due, fmtMoney, fmtTokens, parseStats, view } from '../plugin/hooks/stats'
import type { SyncState } from '../plugin/types'

const dir = join(import.meta.dirname, '..')
const read = (...p: string[]) => readFileSync(join(dir, ...p), 'utf8')
const fixtureNames = readdirSync(join(dir, 'testdata')).filter((f) => f.startsWith('stats-'))
const NOW = Date.parse('2026-11-13T12:00:00Z')
const flat = (v: ReturnType<typeof view>) =>
  v.kind === 'line' ? v.segments.map((s) => s.text).join(' ') : v.kind === 'not_connected' ? v.text : ''

describe('contract', () => {
  const ajv = new Ajv2020({ strict: true })
  addFormats(ajv)
  const validate = ajv.compile(JSON.parse(read('stats.schema.json')))
  test.each(fixtureNames)('%s validates against the schema and parses', (f) => {
    const raw = read('testdata', f)
    expect(validate(JSON.parse(raw)), JSON.stringify(validate.errors)).toBe(true)
    expect(parseStats(raw)).toBeDefined()
  })
  test('the schema rejects drift', () => {
    const ok = JSON.parse(read('testdata', 'stats-ok.json'))
    expect(validate({ ...ok, version: 2 })).toBe(false)
    expect(validate({ ...ok, extra: 1 })).toBe(false)
    expect(validate({ ...ok, sync: { ...ok.sync, state: 'weird' } })).toBe(false)
    const { today: _t, ...noToday } = ok
    expect(validate(noToday)).toBe(false)
  })
})

describe('parseStats', () => {
  test('rejects garbage, other versions and missing parts', () => {
    expect(parseStats('')).toBeUndefined()
    expect(parseStats('not json')).toBeUndefined()
    expect(parseStats('{"version":2,"sync":{"state":"ok","backlog_bytes":0},"today":{}}')).toBeUndefined()
    expect(parseStats('{"version":1,"today":{}}')).toBeUndefined()
    expect(parseStats('{"version":1,"sync":{"state":"nope","backlog_bytes":0},"today":{}}')).toBeUndefined()
  })
})

describe('format', () => {
  test('money', () => {
    expect(fmtMoney(0)).toBe('$0.00')
    expect(fmtMoney(0.004)).toBe('<$0.01')
    expect(fmtMoney(4.256)).toBe('$4.26')
  })
  test('tokens', () => {
    expect([999, 1200, 3_400_000].map(fmtTokens)).toEqual(['999', '1.2k', '3.4M'])
  })
  test('countdown', () => {
    expect(countdown('2026-11-13T13:05:00Z', NOW)).toBe('resets in 1h 05m')
    expect(countdown('2026-11-13T12:23:00Z', NOW)).toBe('resets in 23m')
    expect(countdown('2026-11-13T11:00:00Z', NOW)).toBeUndefined()
    expect(countdown('garbage', NOW)).toBeUndefined()
  })
  test('due: first run, force, 30 s window, clock going backwards', () => {
    expect(due(undefined, NOW, false)).toBe(true)
    expect(due(NOW - 29_999, NOW, false)).toBe(false)
    expect(due(NOW - 30_000, NOW, false)).toBe(true)
    expect(due(NOW - 1, NOW, true)).toBe(true)
    expect(due(NOW + 5_000, NOW, false)).toBe(true)
  })
  test('dot colours', () => {
    const c: Record<SyncState, string> = {
      ok: 'success',
      backlog: 'warning',
      blocked: 'error',
      offline: 'error',
      logged_out: 'error',
    }
    for (const [s, col] of Object.entries(c)) expect(dotColor(s as SyncState)).toBe(col)
  })
})

describe('view per display state', () => {
  const stats = (f: string) => parseStats(read('testdata', f))
  const local = { cost: 0.5, tokens: 900 }

  test('ok: session, today, green dot, limit countdown', () => {
    const v = view(stats('stats-ok.json'), local, NOW)
    expect(v.kind).toBe('line')
    const s = flat(v)
    expect(s).toContain('$0.50')
    expect(s).toContain('900')
    expect(s).toContain('today $4.25')
    expect(s).toContain('3 sessions')
    expect(s).toMatch(/resets in/)
    if (v.kind === 'line') expect(v.segments.find((x) => x.text === '●')?.color).toBe('success')
  })
  test('backlog: yellow dot and a behind note', () => {
    const v = view(stats('stats-backlog.json'), local, NOW)
    if (v.kind !== 'line') throw new Error('line')
    expect(v.segments.find((x) => x.text === '●')?.color).toBe('warning')
    expect(flat(v)).toMatch(/behind/)
  })
  test('blocked: red dot and a blocked note', () => {
    const v = view(stats('stats-blocked.json'), local, NOW)
    if (v.kind !== 'line') throw new Error('line')
    expect(v.segments.find((x) => x.text === '●')?.color).toBe('error')
    expect(flat(v)).toContain('blocked')
  })
  test('offline: cached figures dim and marked stale', () => {
    const v = view(stats('stats-offline.json'), local, NOW)
    if (v.kind !== 'line') throw new Error('line')
    expect(flat(v)).toContain('stale')
    expect(v.segments.find((x) => x.text.startsWith('today'))?.dim).toBe(true)
  })
  test('logged_out and not installed (undefined): dim hint, no figures', () => {
    for (const s of [stats('stats-logged-out.json'), undefined]) {
      const v = view(s, local, NOW)
      expect(v).toEqual({ kind: 'not_connected', text: 'braid: not connected' })
    }
  })
  test('no local usage falls back to the server session; none at all omits the session part', () => {
    expect(flat(view(stats('stats-ok.json'), undefined, NOW))).toContain('$0.50')
    const ok = stats('stats-ok.json')
    if (!ok) throw new Error('fixture')
    expect(flat(view({ ...ok, session: undefined }, undefined, NOW))).not.toContain('$0.50')
  })
  test('an expired limit shows no countdown', () => {
    expect(flat(view(stats('stats-ok.json'), local, Date.parse('2027-01-01T00:00:00Z')))).not.toMatch(/resets in/)
  })
})
