// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { renderApp } from '../test/render'
import { FilterBar } from './FilterBar'

vi.mock('../api/client', async (orig) => ({
  ...(await orig<typeof import('../api/client')>()),
  get: vi.fn(async (path: string) =>
    path.endsWith('/devices')
      ? [
          { id: 'd1', name: 'laptop' },
          { id: 'd2', name: 'desktop' },
        ]
      : {
          projects: [{ path: '/work/alpha', name: 'alpha' }],
          models: ['claude-opus-5-5'],
          skills: ['superpowers:writing-plans'],
        },
  ),
}))

it('writes selections to the URL and reads them back', async () => {
  renderApp(<FilterBar />, { route: '/timeline?range=7d' })
  expect(screen.getByRole('button', { name: '7d' })).toHaveAttribute('aria-pressed', 'true')
  const model = await screen.findByLabelText('Model')
  await waitFor(() => expect(screen.getByRole('option', { name: 'Opus 5.5' })).toBeInTheDocument())
  await userEvent.selectOptions(model, 'claude-opus-5-5')
  expect(model).toHaveValue('claude-opus-5-5')
  await userEvent.selectOptions(screen.getByLabelText('Device'), 'd2')
  expect(screen.getByLabelText('Device')).toHaveValue('d2')
  await userEvent.click(screen.getByRole('button', { name: /reset/i }))
  expect(screen.getByLabelText('Model')).toHaveValue('')
})
