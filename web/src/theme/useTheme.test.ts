// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { act, renderHook } from '@testing-library/react'
import { expect, it } from 'vitest'
import { setTheme } from './theme'
import { useTheme } from './useTheme'

it('follows theme changes', async () => {
  setTheme('dark')
  const { result } = renderHook(() => useTheme())
  expect(result.current).toBe('dark')
  await act(async () => {
    setTheme('light')
    await Promise.resolve()
  })
  expect(result.current).toBe('light')
})
