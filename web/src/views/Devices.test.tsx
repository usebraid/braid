// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { renderApp } from '../test/render'
import { Devices } from './Devices'

const dev = (id: string, over = {}) => ({
  id,
  name: `dev-${id}`,
  hostname: 'h',
  os: 'linux',
  arch: 'amd64',
  os_user: 'me',
  daemon_version: '0.2.0',
  created_at: '2026-10-01T00:00:00Z',
  last_seen_at: null,
  last_sync_at: '2026-11-12T00:00:00Z',
  revoked_at: null,
  stale: false,
  ...over,
})
const api = vi.fn(async (method: string) =>
  method === 'GET'
    ? [dev('a'), dev('b', { stale: true }), dev('c', { revoked_at: '2026-11-01T00:00:00Z' })]
    : method === 'PATCH'
      ? dev('a', { name: 'renamed' })
      : undefined,
)
vi.mock('../api/client', async (orig) => ({
  ...(await orig<typeof import('../api/client')>()),
  get: vi.fn(async () => [dev('a'), dev('b', { stale: true }), dev('c', { revoked_at: '2026-11-01T00:00:00Z' })]),
  api: (...a: unknown[]) => api(...(a as [string])),
}))

it('flags stale and revoked devices, renames and revokes after confirmation', async () => {
  renderApp(<Devices />)
  expect(await screen.findByText('dev-a')).toBeInTheDocument()
  expect(screen.getByText(/stale/i)).toBeInTheDocument()
  expect(screen.getByText('revoked')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: /revoke dev-c/i })).toBeNull()
  await userEvent.click(screen.getByRole('button', { name: /rename dev-a/i }))
  await userEvent.clear(screen.getByLabelText('Device name'))
  await userEvent.type(screen.getByLabelText('Device name'), 'renamed{Enter}')
  expect(api).toHaveBeenCalledWith('PATCH', '/api/v1/devices/a', { body: { name: 'renamed' } })
  await userEvent.click(screen.getByRole('button', { name: /revoke dev-b/i }))
  await userEvent.click(screen.getByRole('button', { name: /confirm revoke/i }))
  expect(api).toHaveBeenCalledWith('DELETE', '/api/v1/devices/b')
})
