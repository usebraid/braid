// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { type RenderResult, render } from '@testing-library/react'
import type { ReactNode } from 'react'
import { MemoryRouter } from 'react-router-dom'
import { LiveProvider, type LiveSource } from '../api/live'
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

// An inert live client, so tests never open a socket; pass `live` to drive events.
const IDLE: LiveSource = { status: 'open', start() {}, stop() {}, subscribe: () => () => {}, onStatus: () => () => {} }

export function renderApp(
  ui: ReactNode,
  o: { route?: string; me?: Partial<Me>; live?: LiveSource } = {},
): RenderResult {
  return render(
    <MemoryRouter initialEntries={[o.route ?? '/']}>
      <MeProvider initial={{ ...ME, ...o.me }}>
        <LiveProvider client={o.live ?? IDLE}>{ui}</LiveProvider>
      </MeProvider>
    </MemoryRouter>,
  )
}
