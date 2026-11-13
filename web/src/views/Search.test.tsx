// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { renderApp } from '../test/render'
import { Search } from './Search'

const hits = {
  hits: [
    {
      session_id: 's1',
      agent_id: 'main',
      role: 'user',
      at: '2026-11-01T10:00:00Z',
      title: 'Alpha work',
      snippet: '…Alpha prompt about zebra…',
      rank: 1,
      line_id: 12,
    },
  ],
}
const get = vi.fn(async (p: string) =>
  p.endsWith('/search') ? hits : p.endsWith('/devices') ? [] : { projects: [], models: [], skills: [] },
)
vi.mock('../api/client', async (orig) => ({
  ...(await orig<typeof import('../api/client')>()),
  get: (...a: unknown[]) => get(...(a as [string])),
}))

it('searches after a pause, highlights the word and links to the line', async () => {
  renderApp(<Search />)
  await userEvent.type(screen.getByLabelText('Search prompts and replies'), 'zebra')
  const link = await screen.findByRole('link', { name: 'Alpha work' })
  expect(link).toHaveAttribute('href', '/sessions/s1?agent=main&line=12')
  expect(screen.getByText('zebra', { selector: 'mark' })).toBeInTheDocument()
  const searchCalls = get.mock.calls.filter((c) => c[0].endsWith('/search'))
  expect(searchCalls.length).toBe(1) // debounced: not one call per keystroke
})

it('does not search below two characters', async () => {
  get.mockClear()
  renderApp(<Search />)
  await userEvent.type(screen.getByLabelText('Search prompts and replies'), 'z')
  await new Promise((r) => setTimeout(r, 450))
  expect(get.mock.calls.filter((c) => c[0].endsWith('/search'))).toHaveLength(0)
  expect(screen.getByText(/at least 2 characters/i)).toBeInTheDocument()
})
