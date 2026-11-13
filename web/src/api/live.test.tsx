// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { act, render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'
import type { LiveStatus } from '../lib/live'
import { LiveBanner, LiveProvider, type LiveSource } from './live'

it('shows the banner only while paused', () => {
  let push: (s: LiveStatus) => void = () => {}
  const client: LiveSource = {
    status: 'open',
    start() {},
    stop() {},
    subscribe: () => () => {},
    onStatus: (f) => {
      push = f
      return () => {}
    },
  }
  render(
    <LiveProvider client={client}>
      <LiveBanner />
    </LiveProvider>,
  )
  expect(screen.getByRole('status')).toBeEmptyDOMElement()
  act(() => push('paused'))
  expect(screen.getByRole('status')).toHaveTextContent('Live updates paused, reconnecting…')
  act(() => push('open'))
  expect(screen.getByRole('status')).toBeEmptyDOMElement()
})
