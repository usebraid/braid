// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { expect, test } from '@playwright/test'
import { cspViolations, loginAs } from './helpers'

test('sign out posts the form, ends the session and leaves through the IdP logout', async ({ page, request }) => {
  await loginAs(page, request, 'alice')
  const csp = cspViolations(page)
  await page.route('**/auth/login**', (r) => r.abort()) // the mock IdP would sign straight back in
  const posted = page.waitForRequest((r) => r.url().endsWith('/auth/logout') && r.method() === 'POST')
  await page.getByRole('button', { name: 'Sign out' }).click()
  expect(await (await posted).headerValue('origin')).toBe('http://localhost:4173')
  await expect(page).toHaveURL(/\/end_session\?/)
  expect((await page.request.get('/api/v1/me')).status()).toBe(401)
  expect(csp).toEqual([])
})

test('the mobile menu offers sign out and the brand links home', async ({ page, request }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await loginAs(page, request, 'alice')
  await page.goto('/devices')
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeHidden()
  await page.getByRole('button', { name: 'Menu' }).click()
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible()
  await page.getByRole('link', { name: 'Braid, go to overview' }).click()
  await expect(page).toHaveURL(/\/$/)
})
