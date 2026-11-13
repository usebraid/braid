// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { expect, it, vi } from 'vitest'
import { Chip, ConfirmDialog, ErrorNote, KpiCard, Segmented } from './ui'

it('KpiCard shows label, value and note', () => {
  render(<KpiCard label="Cost" value="$1.23" note="last 30 days" colors={['#ffd166', '#ff8a3d']} />)
  expect(screen.getByText('Cost')).toBeInTheDocument()
  expect(screen.getByText('$1.23')).toBeInTheDocument()
  expect(screen.getByText('last 30 days')).toBeInTheDocument()
})

it('KpiCard shows the change against the previous period, up green and down red', () => {
  const { rerender } = render(<KpiCard label="Cost" value="$2" delta={25} colors={['#ffd166', '#ff8a3d']} />)
  expect(screen.getByText('↑25%')).toHaveClass('delta', 'up')
  rerender(<KpiCard label="Cost" value="$2" delta={-40} colors={['#ffd166', '#ff8a3d']} />)
  expect(screen.getByText('↓40%')).toHaveClass('delta', 'down')
  rerender(<KpiCard label="Cost" value="$2" delta={null} colors={['#ffd166', '#ff8a3d']} />)
  expect(screen.queryByText(/%$/)).toBeNull()
})

it('Segmented marks the active option and reports changes by keyboard', async () => {
  const onChange = vi.fn()
  render(
    <Segmented
      label="Period"
      value="7d"
      onChange={onChange}
      options={[
        { value: '7d', label: '7d' },
        { value: '30d', label: '30d' },
      ]}
    />,
  )
  expect(screen.getByRole('button', { name: '7d' })).toHaveAttribute('aria-pressed', 'true')
  await userEvent.tab()
  await userEvent.keyboard('{Tab}{Enter}')
  expect(onChange).toHaveBeenCalledWith('30d')
})

it('Chip toggles via aria-pressed and ErrorNote is an alert with retry', async () => {
  const click = vi.fn()
  const retry = vi.fn()
  render(
    <>
      <Chip pressed={false} onClick={click}>
        alpha
      </Chip>
      <ErrorNote error={new Error('boom')} retry={retry} />
    </>,
  )
  expect(screen.getByRole('button', { name: /alpha/ })).toHaveAttribute('aria-pressed', 'false')
  await userEvent.click(screen.getByRole('button', { name: /alpha/ }))
  expect(click).toHaveBeenCalled()
  expect(screen.getByRole('alert')).toHaveTextContent('boom')
  await userEvent.click(screen.getByRole('button', { name: /retry/i }))
  expect(retry).toHaveBeenCalled()
})

function Host() {
  const [open, setOpen] = useState(false)
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        Delete
      </button>
      {open ? (
        <ConfirmDialog title="Sure?" confirmLabel="Yes" onConfirm={() => {}} onCancel={() => setOpen(false)} />
      ) : null}
    </>
  )
}

it('ConfirmDialog returns focus to the button that opened it', async () => {
  render(<Host />)
  const opener = screen.getByRole('button', { name: 'Delete' })
  await userEvent.click(opener)
  await userEvent.click(await screen.findByRole('button', { name: 'Cancel', hidden: true }))
  expect(document.activeElement).toBe(opener)
})
