// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { renderApp } from '../test/render'
import { Failures } from './Failures'

const page = {
  items: [
    {
      category: 'usage_limit',
      detail: 'limit hit',
      occurred_at: '2026-11-01T10:05:00Z',
      session_id: 'g1',
      agent_id: 'a2',
      line_id: 77,
      limit_type: 'five_hour',
      resets_at: '2026-11-01T11:05:00Z',
      time_lost_s: 3600,
    },
  ],
  next: null,
  by_category: [
    { key: 'usage_limit', count: 1, time_lost_s: 3600 },
    { key: 'tool_error', count: 2, time_lost_s: 0 },
  ],
  by_limit_type: [{ key: 'five_hour', count: 1, time_lost_s: 3600 }],
  by_day: [{ key: '2026-11-01', count: 3, time_lost_s: 3600 }],
  time_lost_s: 3600,
}
vi.mock('../api/client', async (orig) => ({
  ...(await orig<typeof import('../api/client')>()),
  get: vi.fn(async (p: string) =>
    p.endsWith('/failures') ? page : p.endsWith('/devices') ? [] : { projects: [], models: [], skills: [] },
  ),
}))

it('summarises failures and links each to its transcript position', async () => {
  renderApp(<Failures />)
  expect(await screen.findByText('Usage limit')).toBeInTheDocument()
  expect(screen.getByText('Failed tool call')).toBeInTheDocument()
  expect(screen.getByText('5-hour limit')).toBeInTheDocument()
  expect(screen.getAllByText('1h 0m').length).toBeGreaterThan(0)
  expect(screen.getByRole('link', { name: /jump to position/i })).toHaveAttribute(
    'href',
    '/sessions/g1?agent=a2&line=77',
  )
})
