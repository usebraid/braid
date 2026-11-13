// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { type RenderResult, render } from '@testing-library/react'
import type { ReactNode } from 'react'
import { MemoryRouter } from 'react-router-dom'
import { MeProvider } from '../api/session'
import type { Me } from '../api/types'

const ME: Me = {
  id: '00000000-0000-4000-8000-000000000001',
  email: 'u@example.com',
  name: 'User',
  is_admin: false,
  time_zone: 'UTC',
  time_zone_set: true,
}

// The live-events provider joins here in Task 12.
export function renderApp(ui: ReactNode, o: { route?: string; me?: Partial<Me> } = {}): RenderResult {
  return render(
    <MemoryRouter initialEntries={[o.route ?? '/']}>
      <MeProvider initial={{ ...ME, ...o.me }}>{ui}</MeProvider>
    </MemoryRouter>,
  )
}
