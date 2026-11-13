// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { renderApp } from '../test/render'
import { Audit } from './Audit'

const e = (id: number, action: string) => ({
  id,
  at: '2026-11-01T10:00:00Z',
  action,
  actor_user_id: null,
  subject_user_id: null,
  device_id: null,
  ip: '203.0.113.5',
  user_agent: 'Mozilla/5.0',
  detail: { reason: 'x' },
})
const get = vi.fn(async (_p: string, q?: Record<string, unknown>) =>
  q?.before
    ? { entries: [e(1, 'login')], next: null }
    : { entries: [e(3, 'device.revoke'), e(2, 'device.rename')], next: 2 },
)
vi.mock('../api/client', async (orig) => ({
  ...(await orig<typeof import('../api/client')>()),
  get: (...a: unknown[]) => get(...(a as [string])),
}))

it('pages the audit log with the id cursor', async () => {
  renderApp(<Audit />)
  expect(await screen.findByText('device.revoke')).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: /load more/i }))
  expect(await screen.findByText('login')).toBeInTheDocument()
  expect(get.mock.calls.at(-1)?.[1]).toMatchObject({ before: 2 })
})
