// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// web/src/views/Session.test.tsx
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { renderApp } from '../test/render'
import { Session } from './Session'

const usage = { cost_usd: 0.01, tokens: 100, input: 1, output: 1, cache_read: 0, cache_write: 0, messages: 1 }
const ag = (id: string, parent: string | null) => ({
  agent_id: id,
  agent_type: id === 'main' ? 'main' : 'Explore',
  description: `d-${id}`,
  parent_agent_id: parent,
  spawn_tool_use_id: null,
  spawn_depth: 0,
  started_at: null,
  ended_at: null,
  finished: null,
  duration_ms: 5000,
  duration_estimated: false,
  tool_calls: { Bash: 2 },
  tool_errors: 1,
  usage,
})
const detail = {
  session: {
    session_id: 'g1',
    title: 'Golden custom',
    project_name: 'alpha',
    models: ['claude-opus-5-5'],
    device_ids: ['d1'],
    is_live: false,
    agent_ms: 5000,
    cost_usd: 0.04,
    started_at: '2026-11-01T10:00:00Z',
    ended_at: '2026-11-01T10:16:00Z',
  },
  agents: [ag('main', null), ag('a1', 'main'), ag('a2', 'main'), ag('a3', 'a1')],
  skill_phases: [{ agent_id: 'main', seq: 1, skill: 'superpowers:brainstorming', started_at: null, ended_at: null }],
  failures: [
    {
      category: 'rate_limit',
      detail: '',
      occurred_at: '2026-11-01T10:05:00Z',
      session_id: 'g1',
      agent_id: 'a2',
      line_id: 5,
      limit_type: null,
      resets_at: null,
      time_lost_s: null,
    },
  ],
}
vi.mock('../api/client', async (orig) => ({
  ...(await orig<typeof import('../api/client')>()),
  get: vi.fn(async (p: string) => (p.endsWith('/devices') ? [] : detail)),
}))

it('draws main plus nested subagents, phase bands and failure markers, and selects an agent from the list', async () => {
  renderApp(<Session />, { route: '/sessions/g1' })
  expect(await screen.findByRole('heading', { name: 'Golden custom' })).toBeInTheDocument()
  for (const id of ['main', 'a1', 'a2', 'a3']) expect(await screen.findByTestId(`agent-node-${id}`)).toBeInTheDocument()
  expect(screen.getByTestId('agent-node-main')).toHaveTextContent('brainstorming')
  expect(screen.getByTestId('agent-node-a2')).toHaveTextContent('1 failure')
  await userEvent.click(screen.getByRole('button', { name: /d-a3/ }))
  expect(screen.getByTestId('agent-node-a3')).toHaveAttribute('aria-current', 'true')
})
