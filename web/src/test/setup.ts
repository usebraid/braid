// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach, vi } from 'vitest'

afterEach(cleanup)

vi.stubGlobal('matchMedia', (query: string) => ({
  matches: false,
  media: query,
  addEventListener: () => {},
  removeEventListener: () => {},
  addListener: () => {},
  removeListener: () => {},
  dispatchEvent: () => false,
  onchange: null,
}))
vi.stubGlobal(
  'ResizeObserver',
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  },
)
// React Flow reads the zoom from a parsed CSS transform
vi.stubGlobal(
  'DOMMatrixReadOnly',
  class {
    m22: number
    constructor(t?: string) {
      this.m22 = Number(/scale\(([\d.]+)\)/.exec(t ?? '')?.[1] ?? 1)
    }
  },
)
