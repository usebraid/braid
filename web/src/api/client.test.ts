// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, api, get } from './client'

const reply = (status: number, body?: unknown) =>
  vi.fn().mockResolvedValue(
    new Response(body === undefined ? null : JSON.stringify(body), {
      status,
      headers: { 'Content-Type': 'application/json' },
    }),
  )

afterEach(() => vi.unstubAllGlobals())

describe('api client', () => {
  it('builds the query without empty values and parses JSON', async () => {
    const f = reply(200, { ok: 1 })
    vi.stubGlobal('fetch', f)
    await expect(
      get('/api/v1/stats/overview', { from: '2026-11-01', device: '', project: undefined, limit: 5 }),
    ).resolves.toEqual({ ok: 1 })
    expect(f.mock.calls[0]?.[0]).toBe('/api/v1/stats/overview?from=2026-11-01&limit=5')
  })
  it('sends JSON bodies', async () => {
    const f = reply(200, {})
    vi.stubGlobal('fetch', f)
    await api('PATCH', '/api/v1/me', { body: { time_zone: 'Europe/Berlin' } })
    const init = f.mock.calls[0]?.[1] as RequestInit
    expect(init.method).toBe('PATCH')
    expect(init.body).toBe('{"time_zone":"Europe/Berlin"}')
    expect((init.headers as Record<string, string>)['Content-Type']).toBe('application/json')
  })
  it('resolves undefined on 204', async () => {
    vi.stubGlobal('fetch', reply(204))
    await expect(api('DELETE', '/api/v1/devices/x')).resolves.toBeUndefined()
  })
  it('throws ApiError with the server code', async () => {
    vi.stubGlobal('fetch', reply(403, { error: 'forbidden' }))
    await expect(get('/api/v1/admin/stats')).rejects.toMatchObject({ status: 403, code: 'forbidden' })
  })
  it('sends the browser to login on 401', async () => {
    vi.stubGlobal('fetch', reply(401, { error: 'unauthenticated' }))
    const assign = vi.fn()
    vi.stubGlobal('location', { pathname: '/timeline', search: '?range=7d', assign })
    const settled = vi.fn()
    get('/api/v1/me').then(settled, settled) // never settles: no error flashes before the redirect
    await new Promise((r) => setTimeout(r, 10))
    expect(assign).toHaveBeenCalledWith('/auth/login?next=%2Ftimeline%3Frange%3D7d')
    expect(settled).not.toHaveBeenCalled()
  })
})
