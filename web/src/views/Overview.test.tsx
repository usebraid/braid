// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// web/src/views/Overview.test.tsx
import { screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { renderApp } from '../test/render'
import { Overview } from './Overview'

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
    failures: 4,
    time_lost_s: 5400,
  },
  per_day: [
    { day: '2026-11-01', cost_usd: 5, tokens: 500_000, sessions: 1 },
    { day: '2026-11-02', cost_usd: 7.5, tokens: 1_000_000, sessions: 2 },
  ],
  per_model: [
    { model: 'claude-opus-5-5', cost_usd: 12.5, tokens: 1_500_000, messages: 10, price_unknown: false },
    { model: 'claude-fable-5-2', cost_usd: 0, tokens: 100, messages: 1, price_unknown: true },
  ],
  per_project: [
    { path: '/work/alpha', name: 'alpha', cost_usd: 12.5, tokens: 1_500_000, sessions: 3, agent_ms: 7_500_000 },
  ],
  heatmap: [{ weekday: 2, hour: 14, count: 12 }],
}
vi.mock('../api/client', async (orig) => ({
  ...(await orig<typeof import('../api/client')>()),
  get: vi.fn(async (p: string) => (p.includes('overview') ? overview : { projects: [], models: [], skills: [] })),
}))

it('shows the KPI tiles with formatted values and the data-quality notes', async () => {
  renderApp(<Overview />, { me: { time_zone: 'UTC', time_zone_set: false } })
  expect(await screen.findByText('$12.50')).toBeInTheDocument()
  expect(screen.getByText('1.5M')).toBeInTheDocument()
  expect(screen.getByText(/≈\s*2h 5m/)).toBeInTheDocument()
  expect(screen.getByText('1h 30m')).toBeInTheDocument() // time lost
  expect(screen.getByText(/excludes 1 unpriced model/)).toBeInTheDocument()
  expect(screen.getByText('Opus 5.5')).toBeInTheDocument()
  expect(screen.getByText('no price')).toBeInTheDocument()
  expect(screen.getByRole('link', { name: /settings/i })).toBeInTheDocument()
  expect(screen.getByRole('img', { name: /Cost per day/ })).toBeInTheDocument()
})
