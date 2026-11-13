// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'
import { App } from './App'

it('renders the app root', () => {
  render(<App />)
  expect(screen.getByRole('heading', { name: 'Braid' })).toBeInTheDocument()
})
