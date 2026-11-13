// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { renderApp } from '../test/render'
import { Admin } from './Admin'

const user = (id: string, email: string, over = {}) => ({
  id,
  email,
  name: email,
  is_admin: false,
  disabled_at: null,
  last_login_at: null,
  last_sync_at: null,
  devices: 1,
  stored_bytes: 2048,
  sessions: 3,
  cost_usd: 1.5,
  tokens: 1000,
  last_activity_at: null,
  derive_errors: 0,
  rebuilding: false,
  ...over,
})
const stats = {
  users: [user('me', 'me@x.test', { is_admin: true }), user('u2', 'bob@x.test')],
  totals: { users: 2, devices: 2, sessions: 6, stored_bytes: 4096, cost_usd: 3, tokens: 2000 },
  rebuilding_users: 0,
}
const api = vi.fn(async (..._a: unknown[]) => undefined)
vi.mock('../api/client', async (orig) => ({
  ...(await orig<typeof import('../api/client')>()),
  get: vi.fn(async (p: string) => (p.endsWith('/admin/stats') ? stats : { entries: [], next: null })),
  api: (...a: unknown[]) => api(...(a as [string])),
}))

it('shows aggregates only, protects the own row and requires the exact email to delete', async () => {
  renderApp(<Admin />, { me: { id: 'me', is_admin: true } })
  expect(await screen.findByText('bob@x.test')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: /delete me@x.test/i })).toBeNull()
  await userEvent.click(screen.getByRole('button', { name: /delete bob@x.test/i }))
  const confirm = screen.getByRole('button', { name: /confirm delete/i })
  expect(confirm).toBeDisabled()
  await userEvent.type(screen.getByLabelText('Type the email to confirm'), 'bob@x.test')
  expect(confirm).toBeEnabled()
  await userEvent.click(confirm)
  expect(api).toHaveBeenCalledWith('DELETE', '/api/v1/admin/users/u2', { query: { confirm: 'bob@x.test' } })
})

it('renders nothing of the admin view for non-admins', () => {
  renderApp(<Admin />, { me: { is_admin: false } })
  expect(screen.getByText(/not found/i)).toBeInTheDocument()
})
