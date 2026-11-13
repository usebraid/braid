// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { App } from './App'

vi.mock('./api/client', () => ({
  get: vi.fn((path: string) =>
    Promise.resolve(
      path === '/api/v1/me'
        ? { id: 'u1', email: 'u@example.com', name: 'User', is_admin: false, time_zone: 'UTC', time_zone_set: true }
        : { version: '0.2.0', commit: 'abc', source_url: 'https://example.com/src' },
    ),
  ),
}))

it('loads the user and shows the overview with its navigation', async () => {
  render(<App />)
  expect(await screen.findByRole('heading', { name: 'Overview' })).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Timeline' })).toBeInTheDocument()
  expect(screen.queryByRole('link', { name: 'Admin' })).toBeNull()
})

it('renders NotFound for /admin to a non-admin', async () => {
  window.history.pushState({}, '', '/admin')
  render(<App />)
  expect(await screen.findByRole('heading', { name: 'Not found' })).toBeInTheDocument()
})
