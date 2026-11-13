// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { act, renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { expect, it, vi } from 'vitest'
import type { LiveEvent } from '../lib/live'
import { useApi } from './hooks'
import { LiveContext } from './live'

function harness() {
  const subs = new Set<(e: LiveEvent) => void>()
  const wrapper = ({ children }: { children: ReactNode }) => (
    <LiveContext.Provider
      value={{
        status: 'open',
        subscribe: (f) => {
          subs.add(f)
          return () => subs.delete(f)
        },
      }}
    >
      {children}
    </LiveContext.Provider>
  )
  const emit = (e: LiveEvent) => {
    for (const f of subs) f(e)
  }
  return { wrapper, emit }
}

it('reloads once, debounced, on a matching event and ignores others', async () => {
  const { wrapper, emit } = harness()
  const load = vi.fn().mockResolvedValue(1)
  const { result } = renderHook(
    () => useApi('k', load, { on: (e) => e.type === 'session_updated' && e.session_id === 'g1' }),
    { wrapper },
  )
  await waitFor(() => expect(result.current.data).toBe(1))
  expect(load).toHaveBeenCalledTimes(1)
  act(() => {
    emit({ type: 'session_updated', session_id: 'other' })
  })
  await new Promise((r) => setTimeout(r, 600))
  expect(load).toHaveBeenCalledTimes(1)
  act(() => {
    emit({ type: 'session_updated', session_id: 'g1' })
    emit({ type: 'session_updated', session_id: 'g1' })
  })
  await waitFor(() => expect(load).toHaveBeenCalledTimes(2))
  act(() => {
    emit({ type: 'resync' })
  })
  await waitFor(() => expect(load).toHaveBeenCalledTimes(3))
})

it('skips loading while key is null', async () => {
  const { wrapper } = harness()
  const load = vi.fn()
  renderHook(() => useApi(null, load), { wrapper })
  await new Promise((r) => setTimeout(r, 50))
  expect(load).not.toHaveBeenCalled()
})
