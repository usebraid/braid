// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// web/src/views/Overview.test.tsx
import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { renderApp } from '../test/render'
import { Overview } from './Overview'

const v = (cost_usd: number, tokens: number, sessions: number, agent_ms: number) => ({
  cost_usd,
  tokens,
  sessions,
  agent_ms,
})
const overview = {
  from: '2026-10-01T00:00:00Z',
  to: '2026-12-01T00:00:00Z',
  time_zone: 'UTC',
  reindexing: false,
  kpi: {
    sessions: 3,
    agent_ms: 7_500_000,
    total_agent_ms: 9_000_000,
    agent_ms_estimated: true,
    cost_usd: 12.5,
    cost_unknown_models: ['claude-fable-5-2'],
    tokens: 1_500_000,
    streak_days: 2,
    longest_agent_ms: 5_000_000,
    failures: 4,
    time_lost_s: 5400,
  },
  previous: { sessions: 4, agent_ms: 5_000_000, cost_usd: 10, tokens: 3_000_000 },
  per_day: [
    { day: '2026-11-01', cost_usd: 5, tokens: 500_000, sessions: 1, agent_ms: 2_500_000 },
    { day: '2026-11-30', cost_usd: 7.5, tokens: 1_000_000, sessions: 2, agent_ms: 5_000_000 },
  ],
  per_day_by_project: {
    projects: [
      { key: '/work/alpha', name: 'alpha' },
      { key: '/work/beta', name: 'beta' },
    ],
    days: [
      { day: '2026-11-01', values: [v(5, 500_000, 1, 2_500_000), v(0, 0, 0, 0)] },
      { day: '2026-11-30', values: [v(2.5, 400_000, 1, 1_000_000), v(5, 600_000, 1, 4_000_000)] },
    ],
  },
  per_model: [],
  per_project: [
    { path: '/work/alpha', name: 'alpha', cost_usd: 7.5, tokens: 900_000, sessions: 2, agent_ms: 3_500_000 },
    { path: '/work/beta', name: 'beta', cost_usd: 5, tokens: 600_000, sessions: 1, agent_ms: 4_000_000 },
  ],
  heatmap: [],
  activity: [
    { day: '2026-11-01', agent_ms: 2_500_000 },
    { day: '2026-11-30', agent_ms: 5_000_000 },
  ],
}
const session = {
  session_id: 's1',
  title: 'Fix the parser',
  project: '/work/beta',
  project_name: 'beta',
  device_id: null,
  device_ids: [],
  models: ['claude-opus-5-5'],
  started_at: '2026-11-30T09:05:00Z',
  ended_at: '2026-11-30T10:00:00Z',
  is_live: false,
  agent_ms: 4_000_000,
  total_agent_ms: 4_000_000,
  agent_ms_estimated: false,
  cost_usd: 5,
  tokens: 600_000,
  failures: 2,
  skills: [],
  line_count: 10,
  git_branch: '',
  turns: 7,
}
vi.mock('../api/client', async (orig) => ({
  ...(await orig<typeof import('../api/client')>()),
  get: vi.fn(async (p: string) =>
    p.includes('overview')
      ? overview
      : p.endsWith('/stats/sessions')
        ? { items: [session], next: null }
        : { projects: [], models: [], skills: [] },
  ),
}))

it('mirrors the board: header, KPI tiles with the change, stream, sessions and activity', async () => {
  renderApp(<Overview />, { me: { time_zone: 'UTC', time_zone_set: false } })
  expect(await screen.findByRole('heading', { level: 1, name: 'All projects' })).toBeInTheDocument()
  expect(screen.getByText('3 sessions since Nov 1, 2026')).toBeInTheDocument()

  const cost = screen.getByTestId('kpi-cost')
  expect(within(cost).getByText('$12.50')).toBeInTheDocument()
  expect(within(cost).getByText('↑25%')).toHaveClass('up')
  expect(cost).toHaveTextContent('vs previous 61 days, excludes 1 unpriced model')
  expect(cost).toHaveAttribute('title', expect.stringContaining('Priced at API rates from the transcripts'))
  expect(within(screen.getByTestId('kpi-tokens')).getByText('↓50%')).toHaveClass('down')
  expect(screen.getByTestId('kpi-time')).toHaveTextContent('2h 5m↑50%estimated')
  expect(screen.getByTestId('kpi-sessions')).toHaveTextContent('3↓25%')

  expect(screen.getByRole('img', { name: /Agent time, last 61 days: total 2h 5m, 61 days/ })).toBeInTheDocument()
  expect(screen.getByRole('link', { name: /settings/i })).toBeInTheDocument()

  const row = (await screen.findByRole('link', { name: 'Fix the parser' })).closest('tr') as HTMLElement
  expect(row).toHaveTextContent('beta · Nov 30, 09:05 · Opus 5.5')
  expect(row).toHaveTextContent('1h 7m$5.00600k72')

  expect(screen.getByTestId('fact-failures')).toHaveTextContent('4')
  expect(screen.getByTestId('fact-lost')).toHaveTextContent('1h 30m')
  expect(screen.getByText('Longest session').nextSibling).toHaveTextContent('1h 23m')
  expect(screen.getByText('Busiest day').nextSibling).toHaveTextContent('Nov 30 · 1h 23m')
  expect(screen.getByText('Active days').parentElement).toHaveTextContent('Active days22%of the last 84 days')
  expect(screen.getByText('Streak').parentElement).toHaveTextContent('Streak1 daylongest 1 day')
})

it('switches the stream metric, hides a project with its chip and filters the session table', async () => {
  renderApp(<Overview />)
  await screen.findByRole('heading', { level: 1, name: 'All projects' })
  await userEvent.click(screen.getByRole('button', { name: 'Cost' }))
  expect(screen.getByRole('img', { name: /Cost, last 61 days: total \$12.50/ })).toBeInTheDocument()
  const beta = screen.getByRole('button', { name: /beta/ })
  await userEvent.click(beta)
  expect(beta).toHaveAttribute('aria-pressed', 'false')
  expect(screen.getByRole('img', { name: /Cost, last 61 days: total \$7.50/ })).toBeInTheDocument()

  await screen.findByRole('link', { name: 'Fix the parser' })
  await userEvent.type(screen.getByRole('searchbox', { name: 'Search sessions' }), 'zebra')
  expect(screen.queryByRole('link', { name: 'Fix the parser' })).toBeNull()
  expect(screen.getByText('No session matches.')).toBeInTheDocument()
})

it('names the filtered project in the crumb and the title', async () => {
  renderApp(<Overview />, { route: '/?project=%2Fwork%2Fbeta' })
  expect(await screen.findByRole('heading', { level: 1, name: 'beta' })).toBeInTheDocument()
  expect(screen.getByText('Overview /')).toHaveTextContent('Overview / beta')
})
