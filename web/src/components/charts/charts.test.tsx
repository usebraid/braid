// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { StreamChart } from './StreamChart'
import { smooth } from './smooth'

const pts = [
  { key: '2026-11-01', label: 'Nov 1', value: 1 },
  { key: '2026-11-02', label: 'Nov 2', value: 3 },
  { key: '2026-11-03', label: 'Nov 3', value: 2 },
]

describe('smooth', () => {
  it('handles short inputs and never overshoots a peak', () => {
    expect(smooth([[0, 5]])).toBe('M0.0,5.0')
    expect(
      smooth([
        [0, 5],
        [10, 7],
      ]),
    ).toBe('M0.0,5.0L10.0,7.0')
    const d = smooth([
      [0, 10],
      [10, 0],
      [20, 10],
    ]) // valley in the middle (SVG y grows downwards)
    const ys = [...d.matchAll(/,(-?\d+\.\d)/g)].map((m) => Number(m[1]))
    expect(Math.min(...ys)).toBeGreaterThanOrEqual(0)
    expect(Math.max(...ys)).toBeLessThanOrEqual(10)
  })
})

describe('StreamChart', () => {
  it('has a text alternative: label, hidden table, focusable columns', () => {
    render(<StreamChart points={pts} format={(v) => `$${v}`} title="Cost per day" />)
    expect(screen.getByRole('img', { name: /Cost per day: total \$6, 3 days/ })).toBeInTheDocument()
    const rows = screen.getAllByRole('row')
    expect(rows).toHaveLength(4) // header + 3
    expect(screen.getByRole('button', { name: 'Nov 2: $3' })).toHaveAttribute('tabindex', '0')
  })
  it('shows the tooltip on keyboard focus and hides it on Escape', async () => {
    render(<StreamChart points={pts} format={(v) => `$${v}`} title="Cost per day" />)
    await userEvent.tab()
    expect(screen.getByRole('status')).toHaveTextContent('Nov 1')
    await userEvent.keyboard('{ArrowRight}')
    expect(screen.getByRole('status')).toHaveTextContent('Nov 2')
    expect(screen.getByRole('status')).toHaveTextContent('$3')
    await userEvent.keyboard('{Escape}')
    expect(screen.getByRole('status')).toBeEmptyDOMElement()
  })
  it('renders the empty message instead of an empty chart', () => {
    render(<StreamChart points={[]} format={String} title="Tokens per day" empty="Nothing yet" />)
    expect(screen.getByText('Nothing yet')).toBeInTheDocument()
  })
  it('stacks layers and breaks the tooltip down by project', async () => {
    const layers = [
      { key: 'a', label: 'alpha', color: '#5fe08a', values: [1, 1, 1] },
      { key: 'b', label: 'beta', color: '#7c8cff', values: [0, 2, 1] },
    ]
    render(<StreamChart points={pts} layers={layers} format={(v) => `$${v}`} title="Cost per day" />)
    await userEvent.tab()
    await userEvent.keyboard('{ArrowRight}')
    expect(screen.getByRole('status')).toHaveTextContent('alpha $1')
    expect(screen.getByRole('status')).toHaveTextContent('beta $2')
  })
})
