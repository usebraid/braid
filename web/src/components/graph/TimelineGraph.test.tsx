// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { renderApp } from '../../test/render'
import { Timeline } from '../../views/Timeline'

const ev = (id: string, kind: string, at: string, over = {}) => ({
  session_id: id,
  kind,
  at,
  device_id: 'd1',
  model: 'claude-opus-5-5',
  project: '/work/alpha',
  project_name: 'alpha',
  title: `Title ${id}`,
  is_live: false,
  failures: 0,
  ...over,
})
const LISTED = {
  session_id: 'l1',
  title: 'Listed',
  project_name: 'alpha',
  started_at: '2026-11-01T10:00:00Z',
  device_ids: [],
  models: [],
  skills: [],
  agent_ms: 1,
  cost_usd: 0,
  tokens: 0,
  failures: 0,
  is_live: false,
}
const get = vi.fn(async (path: string, q?: Record<string, string>) => {
  if (path.endsWith('/devices')) return [{ id: 'd1', name: 'laptop' }]
  if (path.endsWith('/filters')) return { projects: [], models: [], skills: [] }
  if (path.endsWith('/timeline'))
    return q?.cursor
      ? { events: [ev('s3', 'start', '2026-11-12T10:00:00Z')], next: null }
      : {
          events: [
            ev('s1', 'resume', '2026-11-13T00:30:00Z', { failures: 2, is_live: true }),
            ev('s2', 'start', '2026-11-12T22:00:00Z'),
          ],
          next: 'c1',
        }
  return { items: [LISTED], next: null }
})
vi.mock('../../api/client', async (orig) => ({
  ...(await orig<typeof import('../../api/client')>()),
  get: (...a: unknown[]) => get(...(a as [string])),
}))

it('shows the graph by default with kind text, badges, links, paging and the list toggle', async () => {
  renderApp(<Timeline />)
  const link = await screen.findByRole('link', { name: /Title s1/ })
  expect(link).toHaveAttribute('href', '/sessions/s1')
  expect(link).toHaveTextContent('resumed')
  expect(screen.getByRole('link', { name: /Title s2/ })).toHaveTextContent('alpha · laptop · Opus 5.5 · started')
  expect(screen.getByText('2 failures')).toBeInTheDocument()
  expect(screen.getByText('live')).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: /load more/i }))
  expect(await screen.findByRole('link', { name: /Title s3/ })).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'List' }))
  expect(await screen.findByRole('columnheader', { name: 'Session' })).toBeInTheDocument()
  expect(screen.queryByRole('link', { name: /Title s1/ })).toBeNull()
})

it.each([
  ['Pacific/Auckland', 1],
  ['UTC', 2],
])('splits days in the user time zone %s, not UTC', async (time_zone, days) => {
  renderApp(<Timeline />, { me: { time_zone } })
  await screen.findByRole('link', { name: /Title s1/ })
  expect(document.querySelectorAll('.tl-day')).toHaveLength(days)
})
