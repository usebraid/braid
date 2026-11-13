// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { App } from './App'

const ME = { id: 'u1', email: 'u@example.com', name: 'User', is_admin: false, time_zone: 'UTC', time_zone_set: true }
const day = { sessions: 0, cost_usd: 0, tokens: 0 }
const body = (path: string): unknown => {
  if (path === '/api/v1/me') return ME
  if (path === '/api/v1/about') return { version: '0.2.0', commit: 'abc', source_url: 'https://example.com/src' }
  if (path === '/api/v1/devices') return []
  if (path === '/api/v1/stats/live') return { items: [] }
  if (path === '/api/v1/stats/failures')
    return { by_category: [], by_limit_type: [], by_day: [], time_lost_s: 0, items: [], next: null }
  if (path === '/api/v1/stats/filters') return { projects: [], models: [], skills: [] }
  if (path === '/api/v1/stats/sessions') return { items: [], next: null }
  if (path === '/api/v1/stats/overview')
    return {
      kpi: { sessions: 0, cost_unknown_models: [] },
      per_day: [day],
      per_model: [],
      per_project: [],
      heatmap: [],
      time_zone: 'UTC',
    }
  throw new Error(`unmocked ${path}`)
}
vi.mock('./api/client', () => ({ get: vi.fn((path: string) => Promise.resolve(body(path))) }))

it('loads the user and shows the overview with its navigation', async () => {
  render(<App />)
  expect(await screen.findByRole('heading', { name: 'Overview' })).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Timeline' })).toBeInTheDocument()
  expect(screen.queryByRole('link', { name: 'Admin' })).toBeNull()
})

it('routes /timeline to the timeline view', async () => {
  window.history.pushState({}, '', '/timeline')
  render(<App />)
  expect(await screen.findByRole('heading', { name: 'Timeline' })).toBeInTheDocument()
  expect(await screen.findByText('No sessions in this period')).toBeInTheDocument()
})

it('renders NotFound for /admin to a non-admin', async () => {
  window.history.pushState({}, '', '/admin')
  render(<App />)
  expect(await screen.findByRole('heading', { name: 'Not found' })).toBeInTheDocument()
})

const routed: [string, string][] = [
  ['/live', 'Live'],
  ['/failures', 'Failures'],
  ['/search', 'Search'],
  ['/devices', 'Devices'],
  ['/settings', 'Settings'],
]
it.each(routed)('routes %s to its view', async (path, title) => {
  window.history.pushState({}, '', path)
  render(<App />)
  expect(await screen.findByRole('heading', { name: title })).toBeInTheDocument()
})
