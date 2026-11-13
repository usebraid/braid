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
  it('is one tab stop with a text alternative: label and a hidden table of the days with data', () => {
    render(
      <StreamChart
        points={[...pts, { key: '2026-11-04', label: 'Nov 4', value: 0 }]}
        format={(v) => `$${v}`}
        title="Cost per day"
      />,
    )
    expect(screen.getByRole('img', { name: /Cost per day: total \$6, 4 days/ })).toBeInTheDocument()
    expect(screen.getAllByRole('row')).toHaveLength(4) // header + 3, the zero day is left out
    expect(screen.getByRole('group', { name: /Cost per day/ })).toHaveAttribute('tabindex', '0')
    expect(screen.queryAllByRole('button')).toHaveLength(0)
  })
  it('reads the latest day on focus, moves with the arrows and hides on Escape', async () => {
    render(<StreamChart points={pts} format={(v) => `$${v}`} title="Cost per day" />)
    await userEvent.tab()
    expect(screen.getByRole('status')).toHaveTextContent('Nov 3')
    await userEvent.keyboard('{ArrowLeft}')
    expect(screen.getByRole('status')).toHaveTextContent('Nov 2')
    expect(screen.getByRole('status')).toHaveTextContent('$3')
    await userEvent.keyboard('{Home}')
    expect(screen.getByRole('status')).toHaveTextContent('Nov 1')
    await userEvent.keyboard('{Escape}')
    expect(screen.getByRole('status')).toBeEmptyDOMElement()
  })
  it('draws the axis and an empty note when the range has no value, a single day still as a peak', () => {
    const zero = pts.map((q) => ({ ...q, value: 0 }))
    const { container, rerender } = render(<StreamChart points={zero} format={String} title="Tokens per day" />)
    expect(screen.getByText('No sessions in this period')).toBeInTheDocument()
    expect(screen.getByRole('group', { name: /Tokens per day/ })).not.toHaveAttribute('tabindex')
    rerender(
      <StreamChart
        points={zero.map((q, i) => (i === 1 ? { ...q, value: 5 } : q))}
        format={String}
        title="Tokens per day"
      />,
    )
    expect(screen.queryByText('No sessions in this period')).toBeNull()
    expect(screen.queryByText(/Not enough days/)).toBeNull()
    expect(container.querySelectorAll('path').length).toBeGreaterThan(1)
  })
  it('stacks layers, breaks the tooltip down by project and puts one dot per session under the axis', async () => {
    const layers = [
      { key: 'a', label: 'alpha', color: '#5fe08a', values: [1, 1, 1] },
      { key: 'b', label: 'beta', color: '#7c8cff', values: [0, 2, 1] },
    ]
    const dots = [
      { i: 0, color: '#5fe08a' },
      { i: 1, color: '#5fe08a' },
      { i: 1, color: '#7c8cff' },
    ]
    const { container } = render(
      <StreamChart points={pts} layers={layers} dots={dots} format={(v) => `$${v}`} title="Cost per day" />,
    )
    expect(container.querySelectorAll('circle')).toHaveLength(3)
    await userEvent.tab()
    await userEvent.keyboard('{ArrowLeft}')
    expect(screen.getByRole('status')).toHaveTextContent('alpha $1')
    expect(screen.getByRole('status')).toHaveTextContent('beta $2')
  })
})
