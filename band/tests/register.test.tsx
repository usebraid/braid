// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, test, vi } from 'vitest'
import { register } from '../hooks/band/register'

const fx = (n: string) => readFileSync(join(import.meta.dirname, '..', 'testdata', n), 'utf8')

// Minimal fake engine: records handlers, a clock, a store-backed atom, and a scripted `braid` process.
function rig(run: (argv: string[]) => Promise<{ exitCode: number; stdout: string }>) {
  const handlers: Record<string, (...a: any[]) => any> = {}
  const on: any = (name: string, a: any, b?: any) => {
    handlers[name] = typeof a === 'function' ? a : b
    return { catch: () => on }
  }
  const clock = { t: Date.parse('2026-11-13T12:00:00Z') }
  const calls: string[][] = []
  const forbidden = vi.fn()
  const $: any = {
    clock: { now: async () => clock.t },
    session: {
      id: async () => 's1',
      usage: async () => ({
        cost: { usd: 0.5 },
        startedAt: clock.t,
        tokens: { input: 400, output: 500, cacheRead: 0, cacheWrite: 0 },
      }),
    },
    process: {
      run: async (argv: string[]) => {
        calls.push(argv)
        return run(argv)
      },
    },
    ui: {
      resolve: () => ({ Box: 'Box', Text: 'Text', Button: 'Button' }),
      toast: forbidden,
      status: forbidden,
      message: forbidden,
    },
    command: { run: forbidden },
    http: { fetch: forbidden },
    state: {} as Record<string, unknown>,
  }
  register(on, {})
  const render = async () => handlers['ui.render']?.($, { props: {}, surface: 'terminal' }, async () => undefined)
  const start = () => handlers['session.start']?.($, {}, async (e: unknown) => e)
  return { $, calls, clock, render, start, forbidden }
}
const text = (node: any): string => JSON.stringify(node)

describe('band module', () => {
  test('session.start runs the CLI immediately with the session id; render is not blocked by it', async () => {
    const r = rig(async () => ({ exitCode: 0, stdout: fx('stats-ok.json') }))
    await r.start()
    expect(r.calls).toEqual([['braid', 'stats', '--json', '--session', 's1']])
    const out = text(await r.render())
    expect(out).toContain('$0.50')
    expect(out).toContain('today')
  })
  test('at most one CLI run per 30 s', async () => {
    const r = rig(async () => ({ exitCode: 0, stdout: fx('stats-ok.json') }))
    await r.start()
    for (let i = 0; i < 5; i++) await r.render()
    r.clock.t += 29_000
    await r.render()
    expect(r.calls.length).toBe(1)
    r.clock.t += 2_000
    await r.render()
    await new Promise((res) => setTimeout(res, 0))
    expect(r.calls.length).toBe(2)
  })
  test('braid not installed: dim hint, no error, no throw', async () => {
    const r = rig(async () => {
      throw new Error('spawn braid ENOENT')
    })
    await r.start()
    expect(text(await r.render())).toContain('braid: not connected')
  })
  test('logged out and a broken CLI both show the hint', async () => {
    for (const run of [
      async () => ({ exitCode: 0, stdout: fx('stats-logged-out.json') }),
      async () => ({ exitCode: 1, stdout: '' }),
      async () => ({ exitCode: 0, stdout: 'garbage' }),
    ]) {
      const r = rig(run)
      await r.start()
      expect(text(await r.render())).toContain('braid: not connected')
    }
  })
  test('offline keeps the cached figures, marked stale', async () => {
    const r = rig(async () => ({ exitCode: 0, stdout: fx('stats-offline.json') }))
    await r.start()
    expect(text(await r.render())).toContain('stale')
  })
  test('zero tokens: the module never talks to the model or the network', async () => {
    const r = rig(async () => ({ exitCode: 0, stdout: fx('stats-ok.json') }))
    await r.start()
    await r.render()
    expect(r.forbidden).not.toHaveBeenCalled()
  })
})
