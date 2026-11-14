// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { expect, test } from '@playwright/test'
import { cspViolations, loginAs, RANGE } from './helpers'

test.describe('views as alice', () => {
  test.beforeEach(async ({ page, request }) => {
    await loginAs(page, request, 'alice')
  })

  test('overview: KPI tiles, stream, sessions, activity and the project list', async ({ page }) => {
    const csp = cspViolations(page)
    await page.goto(`/?${RANGE}&project=%2Fwork%2Falpha`)
    await expect(page.getByRole('heading', { level: 1, name: 'alpha' })).toBeVisible()
    await expect(page.getByTestId('kpi-sessions')).toContainText('1')
    await expect(page.getByTestId('kpi-cost')).toContainText('$0.04')
    await expect(page.getByTestId('kpi-tokens')).toContainText('5.5k')
    await expect(page.getByTestId('fact-failures')).toContainText('7')
    await expect(page.getByRole('img', { name: /Agent time, last \d+ days/ })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Golden custom' })).toBeVisible()
    await page
      .getByRole('navigation', { name: 'Projects' })
      .getByRole('button', { name: /^All projects/ })
      .click()
    await expect(page.getByRole('heading', { level: 1, name: 'All projects' })).toBeVisible()
    await expect(page.getByTestId('kpi-sessions')).toContainText('5') // g1, b1, big1, r1 and the live session
    expect(csp).toEqual([])
  })

  test('timeline: rows, filter by project, click opens the session', async ({ page }) => {
    await page.goto(`/timeline?${RANGE}&view=list`)
    await expect(page.getByRole('link', { name: 'Golden custom' })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Beta small' })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Bob private' })).toHaveCount(0) // user isolation
    await page.getByLabel('Project', { exact: true }).selectOption('/work/beta')
    await expect(page.getByRole('link', { name: 'Golden custom' })).toHaveCount(0)
    await expect(page.getByRole('link', { name: 'Beta small' })).toBeVisible()
    await page.getByRole('link', { name: 'Beta small' }).click()
    await expect(page).toHaveURL(/\/sessions\/b1/)
  })

  test('failures: categories and jump to position', async ({ page }) => {
    await page.goto(`/failures?${RANGE}`)
    await expect(page.getByText('Usage limit').first()).toBeVisible()
    await expect(page.getByText('Failed tool call').first()).toBeVisible()
    await page
      .getByRole('link', { name: /jump to position/i })
      .first()
      .click()
    await expect(page).toHaveURL(/\/sessions\/g1\?agent=.*&line=\d+/)
    await expect(page.locator('[aria-current="true"]').first()).toBeVisible()
    await expect(page.locator('[aria-current="true"]').first()).toBeFocused()
  })

  test('search: snippet jumps to the line', async ({ page }) => {
    await page.goto(`/search?${RANGE}`)
    await page.getByLabel('Search prompts and replies').fill('Golden prompt')
    await page.getByRole('link', { name: 'Golden custom' }).first().click()
    await expect(page).toHaveURL(/line=\d+/)
    await expect(page.locator('li[aria-current="true"]')).toContainText('Golden prompt')
  })

  test('devices, audit and settings render', async ({ page }) => {
    await page.goto('/devices')
    await expect(page.getByRole('heading', { name: 'Devices' })).toBeVisible()
    await page.goto('/audit')
    await expect(page.getByRole('heading', { name: 'Audit' })).toBeVisible()
    await expect(page.getByText('login').first()).toBeVisible()
    await page.goto('/settings')
    await page.getByLabel('Time zone').selectOption('Europe/Berlin')
    await page.getByRole('button', { name: /save/i }).click()
    await expect(page.getByText('Saved')).toBeVisible() // reloading earlier would cancel the PATCH
    await page.reload()
    await expect(page.getByLabel('Time zone')).toHaveValue('Europe/Berlin')
    await page.getByRole('button', { name: 'Light' }).click()
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  })

  test('admin view is available to the admin', async ({ page }) => {
    await page.goto('/admin')
    await expect(page.getByText('bob', { exact: false }).first()).toBeVisible()
    await expect(page.getByText('Bob private')).toHaveCount(0) // aggregates only
  })
})

test('admin view is hidden from non-admins', async ({ page, request }) => {
  await loginAs(page, request, 'bob')
  await expect(page.getByRole('link', { name: 'Admin' })).toHaveCount(0)
  await page.goto('/admin')
  await expect(page.getByText(/not found/i)).toBeVisible()
  const r = await page.request.get('/api/v1/admin/stats')
  expect(r.status()).toBe(403)
})
