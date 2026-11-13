// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { renderApp } from '../test/render'
import { LiveView } from './LiveView'

const now = new Date().toISOString()
const live = {
  items: [
    {
      session: {
        session_id: 'l1',
        title: 'Live work',
        project_name: 'live',
        device_id: 'd1',
        ended_at: now,
        is_live: true,
      },
      agents: [
        {
          agent_id: 'main',
          agent_type: 'main',
          description: '',
          parent_agent_id: null,
          started_at: now,
          current_tool: { name: 'Bash', summary: 'sleep 60', started_at: now },
        },
        {
          agent_id: 'a1',
          agent_type: 'Explore',
          description: 'look around',
          parent_agent_id: 'main',
          started_at: now,
          current_tool: null,
        },
      ],
    },
  ],
}
vi.mock('../api/client', async (orig) => ({
  ...(await orig<typeof import('../api/client')>()),
  get: vi.fn(async (p: string) => (p.endsWith('/devices') ? [{ id: 'd1', name: 'laptop' }] : live)),
}))

it('shows running sessions with agents and the current tool', async () => {
  renderApp(<LiveView />)
  expect(await screen.findByRole('link', { name: 'Live work' })).toHaveAttribute('href', '/sessions/l1')
  expect(screen.getByText(/Bash/)).toBeInTheDocument()
  expect(screen.getByText(/sleep 60/)).toBeInTheDocument()
  expect(screen.getByText('look around')).toBeInTheDocument()
  expect(screen.getByText(/idle/i)).toBeInTheDocument()
  expect(screen.getByText('laptop', { exact: false })).toBeInTheDocument()
})
