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

const keys = [
  '2026-11-01',
  '2026-11-02',
  '2026-11-03',
  '2026-11-04',
  '2026-11-05',
  '2026-11-06',
  '2026-11-07',
  '2026-11-08',
  '2026-11-09',
]
const label = (k: string) => `Nov ${Number(k.slice(8))}`

it('Honeycomb is one tab stop, reads the busiest day on focus and moves by day and week', async () => {
  const values = [0, 3, 0, 0, 0, 0, 0, 0, 1]
  render(<Honeycomb title="Activity" keys={keys} values={values} label={label} format={(v) => `${v}h`} />)
  const group = screen.getByRole('group', { name: 'Activity' })
  await userEvent.tab()
  expect(group).toHaveFocus()
  const live = () => screen.getByText(/Nov/, { selector: '[aria-live]' })
  expect(live()).toHaveTextContent('Nov 2: 3h')
  await userEvent.keyboard('{ArrowDown}')
  expect(live()).toHaveTextContent('Nov 3: no session')
  await userEvent.keyboard('{ArrowRight}')
  expect(live()).toHaveTextContent('Nov 3: no session') // a week on is past the last day: stays
  await userEvent.keyboard('{ArrowUp}{ArrowUp}{ArrowRight}')
  expect(live()).toHaveTextContent('Nov 8: no session')
  expect(screen.getAllByRole('row')).toHaveLength(3) // header + 2 days with time
})

it('Honeycomb draws every day, empty ones in the empty colour', () => {
  const { container } = render(
    <Honeycomb title="A" keys={keys} values={keys.map(() => 0)} label={label} format={String} />,
  )
  expect(container.querySelectorAll('path.cell-empty')).toHaveLength(9)
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
