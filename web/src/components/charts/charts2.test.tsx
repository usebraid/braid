// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it } from 'vitest'
import { BarList } from './BarList'
import { Honeycomb } from './Honeycomb'
import { heatColor, hexPath } from './heat'

it('heat ramp runs from indigo to yellow and clamps', () => {
  expect(heatColor(0)).toBe('rgb(58,48,120)')
  expect(heatColor(1)).toBe('rgb(255,214,102)')
  expect(heatColor(2)).toBe('rgb(255,214,102)')
  expect(hexPath(0, 0, 10).startsWith('M0.0,-10.0')).toBe(true)
})

it('Honeycomb is one tab stop, moves with arrows and announces the cell', async () => {
  render(
    <Honeycomb
      title="Activity"
      unit="messages"
      cells={[
        { weekday: 2, hour: 14, count: 12 },
        { weekday: 3, hour: 9, count: 4 },
      ]}
    />,
  )
  const group = screen.getByRole('group', { name: 'Activity' })
  await userEvent.tab()
  expect(group).toHaveFocus()
  await userEvent.keyboard('{ArrowDown}')
  const live = screen.getByText(/messages|no activity/i, { selector: '[aria-live]' })
  expect(live).toBeInTheDocument()
  expect(screen.getAllByRole('row').length).toBe(3) // header + 2 non-empty cells
})

it('Honeycomb reads the busiest cell on focus and the neighbour after an arrow', async () => {
  render(<Honeycomb title="Activity" unit="messages" cells={[{ weekday: 2, hour: 14, count: 12 }]} />)
  await userEvent.tab()
  expect(screen.getByText(/Tuesday 14:00, 12 messages/, { selector: '[aria-live]' })).toBeInTheDocument()
  await userEvent.keyboard('{ArrowRight}')
  expect(screen.getByText('Tuesday 15:00, no activity', { selector: '[aria-live]' })).toBeInTheDocument()
})

it('BarList scales bars to the maximum and lists every row', () => {
  render(
    <BarList
      title="Cost by model"
      rows={[
        { key: 'a', label: 'Opus 5.5', value: 4, text: '$4.00', color: '#5fe08a' },
        { key: 'b', label: 'Sonnet 5.5', value: 1, text: '$1.00', color: '#7c8cff', note: 'no price' },
      ]}
    />,
  )
  const items = screen.getAllByRole('listitem')
  expect(items).toHaveLength(2)
  expect(items[0]).toHaveTextContent('Opus 5.5')
  expect(items[1]).toHaveTextContent('no price')
  expect(items[1]?.querySelector('[data-share]')).toHaveAttribute('data-share', '0.25')
})
