// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, expect, it, vi } from 'vitest'
import { Legend } from './Legend'

const items = [{ key: 'bg', label: 'Background agent', shape: 'dotted' as const, color: 'red' }]
beforeEach(() => localStorage.clear())

it('lists its items, collapses, and remembers the choice per browser', async () => {
  const first = render(<Legend items={items} />)
  expect(screen.getByText('Background agent')).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: /Legend/ }))
  expect(screen.queryByText('Background agent')).toBeNull()
  expect(screen.getByRole('button', { name: /Legend/ })).toHaveAttribute('aria-expanded', 'false')
  first.unmount()
  render(<Legend items={items} />)
  expect(screen.queryByText('Background agent')).toBeNull()
})

it('renders nothing without items and survives blocked storage', async () => {
  const { container } = render(<Legend items={[]} />)
  expect(container).toBeEmptyDOMElement()
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
    throw new Error('blocked')
  })
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
    throw new Error('blocked')
  })
  render(<Legend items={items} />)
  await userEvent.click(screen.getByRole('button', { name: /Legend/ }))
  expect(screen.queryByText('Background agent')).toBeNull()
  vi.restoreAllMocks()
})
