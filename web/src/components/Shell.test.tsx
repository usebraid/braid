// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, expect, it, vi } from 'vitest'
import { renderApp } from '../test/render'
import { Shell } from './Shell'

vi.mock('../api/client', async (orig) => ({
  ...(await orig<typeof import('../api/client')>()),
  get: vi.fn(async (path: string) =>
    path === '/api/v1/stats/filters'
      ? {
          projects: [
            { path: '/work/alpha', name: 'alpha', sessions: 3 },
            { path: '/work/beta', name: 'beta', sessions: 2 },
          ],
          models: [],
          skills: [],
        }
      : { version: '0.2.0', commit: 'abc123', source_url: 'https://github.com/usebraid/braid/tree/abc123' },
  ),
}))
beforeEach(() => vi.clearAllMocks())

it('hides Admin from non-admins and shows it to admins', () => {
  const { unmount } = renderApp(<Shell />, { me: { is_admin: false } })
  expect(screen.queryByRole('link', { name: 'Admin' })).toBeNull()
  unmount()
  renderApp(<Shell />, { me: { is_admin: true } })
  expect(screen.getByRole('link', { name: 'Admin' })).toBeInTheDocument()
})

it('footer links to the source of the running version and credits both projects', async () => {
  renderApp(<Shell />)
  const src = await screen.findByRole('link', { name: /source/i })
  await waitFor(() => expect(src).toHaveAttribute('href', 'https://github.com/usebraid/braid/tree/abc123'))
  expect(screen.getByText(/Built on/)).toHaveTextContent('agents-observe')
  expect(screen.getByText(/Built on/)).toHaveTextContent('ponytail')
})

it('has a skip link as the first tab stop and a labelled main region', () => {
  renderApp(<Shell />)
  expect(screen.getAllByRole('link')[0]).toHaveTextContent('Skip to content')
  expect(screen.getByRole('main')).toHaveAttribute('id', 'main')
})

it('links the brand to the overview and signs out with a same-origin form POST', () => {
  renderApp(<Shell />, { me: { name: 'Ada Lovelace' } })
  expect(screen.getByRole('link', { name: 'Braid, go to overview' })).toHaveAttribute('href', '/')
  const button = screen.getByRole('button', { name: 'Sign out' })
  const form = button.closest('form')
  expect(form).toHaveAttribute('method', 'post')
  expect(form).toHaveAttribute('action', '/auth/logout')
  expect(button).toHaveAttribute('type', 'submit')
})

it('shows the proxied avatar and falls back to initials when it does not load', () => {
  const { container } = renderApp(<Shell />, { me: { name: 'Ada Lovelace' } })
  const img = container.querySelector('.avatar img')
  expect(img).toHaveAttribute('src', '/api/v1/me/avatar')
  fireEvent.error(img as Element)
  expect(container.querySelector('.avatar')).toHaveTextContent('AL')
  expect(container.querySelector('.avatar img')).toBeNull()
})

it('lists the projects with their session counts and filters by one on click', async () => {
  renderApp(<Shell />, { route: '/devices' })
  const nav = screen.getByRole('navigation', { name: 'Projects' })
  const alpha = await within(nav).findByRole('button', { name: 'alpha, 3 sessions' })
  expect(within(nav).getByRole('button', { name: 'All projects, 5 sessions' })).toHaveAttribute('aria-pressed', 'true')
  await userEvent.click(alpha)
  expect(alpha).toHaveAttribute('aria-pressed', 'true') // and the view moved to the overview, filtered
  expect(within(nav).getByRole('button', { name: 'All projects, 5 sessions' })).toHaveAttribute('aria-pressed', 'false')
})

it('says how the numbers are measured in the footer', () => {
  renderApp(<Shell />)
  expect(
    screen.getByText(/Measured from Claude Code transcripts. Cost is priced at API rates, not a bill./),
  ).toBeInTheDocument()
})
