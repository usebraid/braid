// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// web/src/components/TranscriptPanel.test.tsx
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { expect, it, vi } from 'vitest'
import { renderApp } from '../test/render'
import { TranscriptPanel } from './TranscriptPanel'

const line = (id: number, text: string) => ({
  id,
  offset: id,
  line: { type: 'user', message: { role: 'user', content: text } },
})
const get = vi.fn(async (_p: string, q: Record<string, string | number>) =>
  q.before
    ? { lines: [line(3, 'earlier one')], next: null, prev: null }
    : { lines: [line(4, 'the failing line'), line(5, 'after')], next: null, prev: '4' },
)
vi.mock('../api/client', async (orig) => ({
  ...(await orig<typeof import('../api/client')>()),
  get: (...a: unknown[]) => get(...(a as [string, Record<string, string | number>])),
}))

it('opens at the focused line (after = id-1), marks it, and loads earlier lines on demand', async () => {
  renderApp(<TranscriptPanel sessionId="g1" agentId="main" focusLine={4} liveTick={0} />)
  const focused = await screen.findByText('the failing line')
  expect(get.mock.calls[0]?.[1]).toMatchObject({ agent: 'main', after: 3 })
  await waitFor(() => expect(focused.closest('article')).toHaveAttribute('aria-current', 'true'))
  await userEvent.click(screen.getByRole('button', { name: /load earlier/i }))
  expect(await screen.findByText('earlier one')).toBeInTheDocument()
  expect(get.mock.calls.at(-1)?.[1]).toMatchObject({ before: 4 })
  expect(screen.queryByRole('button', { name: /load earlier/i })).toBeNull()
})

function Live() {
  const [tick, setTick] = useState(0)
  return (
    <>
      <button type="button" onClick={() => setTick(tick + 1)}>
        tick
      </button>
      <TranscriptPanel sessionId="g1" agentId="main" liveTick={tick} />
    </>
  )
}

it('fetches after the last loaded line when the live tick changes and the end was loaded', async () => {
  renderApp(<Live />)
  await screen.findByText('after')
  await userEvent.click(screen.getByRole('button', { name: 'tick' }))
  await waitFor(() => expect(get.mock.calls.at(-1)?.[1]).toMatchObject({ after: 5 }))
})
