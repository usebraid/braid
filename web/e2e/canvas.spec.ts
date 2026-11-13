// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { expect, test } from '@playwright/test'
import { loginAs, RANGE } from './helpers'

test('canvas shows main and the nested subagents; node click opens the transcript', async ({ page, request }) => {
  await loginAs(page, request, 'alice')
  await page.goto(`/sessions/g1?${RANGE}`)
  for (const id of ['main', 'a1', 'a2', 'a3']) await expect(page.getByTestId(`agent-node-${id}`)).toBeVisible()
  await expect(page.getByTestId('agent-node-main')).toHaveAttribute('title', /brainstorming/)
  await expect(page.locator('.react-flow__minimap')).toBeVisible()
  const x = async (id: string) => (await page.getByTestId(`agent-node-${id}`).boundingBox())?.x ?? Number.NaN
  expect(await x('main')).toBeLessThan(await x('a1')) // left to right
  expect(await x('a1')).toBeLessThan(await x('a3'))
  await page.getByTestId('agent-node-a1').click()
  await expect(page).toHaveURL(/agent=a1/)
  await expect(page.getByRole('complementary', { name: 'Transcript' }).getByText('sub task')).toBeVisible()
})

test('a session with 102 subagents opens readable, groups wide fan-outs and has a list fallback', async ({
  page,
  request,
}) => {
  await loginAs(page, request, 'alice')
  await page.goto(`/sessions/big1?${RANGE}`)
  const main = page.getByTestId('agent-node-main')
  await expect(main).toBeVisible()
  // the first view keeps zoom 1, so node text renders at its CSS size
  const px = await main.locator('.desc').evaluate((el) => el.getBoundingClientRect().height)
  expect(px).toBeGreaterThanOrEqual(15)
  const more = page.getByTestId('more-main')
  await expect(more).toHaveText('+7 more')
  await expect(page.locator('[data-testid^="agent-node-"]')).not.toHaveCount(103)
  await more.focus()
  await page.keyboard.press('Enter')
  await expect(more).toHaveCount(0)
  await page.getByRole('button', { name: 'List', exact: true }).click()
  await expect(page).toHaveURL(/view=list/)
  const rows = page.getByRole('list', { name: 'Agents' }).getByRole('button')
  await expect(rows).toHaveCount(103)
  await rows.nth(5).focus()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/agent=b\d{3}/)
  await expect(page.getByRole('complementary', { name: 'Transcript' })).toBeVisible()
})
