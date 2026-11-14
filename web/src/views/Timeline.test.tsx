// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// web/src/views/Timeline.test.tsx
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { renderApp } from '../test/render'
import { Timeline } from './Timeline'

const item = (id: string, over = {}) => ({
  session_id: id,
  title: `Title ${id}`,
  project: '/work/alpha',
  project_name: 'alpha',
  device_id: 'd1',
  device_ids: ['d1'],
  models: ['claude-opus-5-5'],
  started_at: '2026-11-01T10:00:00Z',
  ended_at: '2026-11-01T10:16:00Z',
  is_live: false,
  agent_ms: 5000,
  total_agent_ms: 8000,
  agent_ms_estimated: false,
  cost_usd: 0.04,
  tokens: 5474,
  failures: 0,
  skills: ['superpowers:writing-plans'],
  line_count: 35,
  git_branch: 'main',
  ...over,
})
const get = vi.fn(async (path: string, q?: Record<string, string>) => {
  if (path.endsWith('/devices')) return [{ id: 'd1', name: 'laptop' }]
  if (path.endsWith('/filters')) return { projects: [], models: [], skills: [] }
  return q?.cursor
    ? { items: [item('s3')], next: null }
    : { items: [item('s1', { failures: 2 }), item('s2', { is_live: true })], next: 'c1' }
})
vi.mock('../api/client', async (orig) => ({
  ...(await orig<typeof import('../api/client')>()),
  get: (...a: unknown[]) => get(...(a as [string])),
}))

it('lists sessions with badges, links to the canvas and loads the next page', async () => {
  renderApp(<Timeline />, { route: '/timeline?view=list' })
  const link = await screen.findByRole('link', { name: 'Title s1' })
  expect(link).toHaveAttribute('href', '/sessions/s1')
  expect(screen.getByText('2 failures')).toBeInTheDocument()
  expect(screen.getByText('live')).toBeInTheDocument()
  expect(screen.getAllByText(/laptop/).length).toBeGreaterThan(0)
  await userEvent.click(screen.getByRole('button', { name: /load more/i }))
  expect(await screen.findByRole('link', { name: 'Title s3' })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: /load more/i })).toBeNull()
})
