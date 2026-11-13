// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import type { APIRequestContext, Page } from '@playwright/test'

export const CONTROL = 'http://localhost:4174'
export const RANGE = 'from=2026-10-01T00:00:00Z&to=2030-01-01T00:00:00Z' // fixtures date from 2026-11-01

export async function loginAs(page: Page, request: APIRequestContext, sub: 'alice' | 'bob') {
  await request.post(`${CONTROL}/user?sub=${sub}${sub === 'alice' ? '&admin=1' : ''}`)
  await page.context().clearCookies()
  await page.goto('/')
  await page.getByRole('main').waitFor()
}

export function cspViolations(page: Page): string[] {
  const seen: string[] = []
  page.on('console', (m) => {
    if (/Content Security Policy/i.test(m.text())) seen.push(m.text())
  })
  return seen
}
