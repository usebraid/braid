// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { renderApp } from '../test/render'
import { Settings } from './Settings'

const api = vi.fn(async (..._a: unknown[]) => ({}))
vi.mock('../api/client', async (orig) => ({
  ...(await orig<typeof import('../api/client')>()),
  api: (...a: unknown[]) => api(...(a as [string])),
}))

it('saves the time zone and switches the theme', async () => {
  renderApp(<Settings />, { me: { time_zone: 'UTC', time_zone_set: false } })
  await userEvent.selectOptions(screen.getByLabelText('Time zone'), 'Europe/Berlin')
  await userEvent.click(screen.getByRole('button', { name: /save/i }))
  expect(api).toHaveBeenCalledWith('PATCH', '/api/v1/me', { body: { time_zone: 'Europe/Berlin' } })
  await userEvent.click(screen.getByRole('button', { name: 'Light' }))
  expect(document.documentElement.getAttribute('data-theme')).toBe('light')
})
