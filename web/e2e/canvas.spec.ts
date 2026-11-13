// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { expect, test } from '@playwright/test'
import { loginAs, RANGE } from './helpers'

test('canvas shows main and the nested subagents; node click opens the transcript', async ({ page, request }) => {
  await loginAs(page, request, 'alice')
  await page.goto(`/sessions/g1?${RANGE}`)
  for (const id of ['main', 'a1', 'a2', 'a3']) await expect(page.getByTestId(`agent-node-${id}`)).toBeVisible()
  await expect(page.getByTestId('agent-node-main')).toContainText('brainstorming')
  await expect(page.locator('.react-flow__minimap')).toBeVisible()
  const y = async (id: string) => (await page.getByTestId(`agent-node-${id}`).boundingBox())?.y ?? Number.NaN
  expect(await y('main')).toBeLessThan(await y('a1'))
  expect(await y('a1')).toBeLessThan(await y('a3'))
  await page.getByTestId('agent-node-a1').click()
  await expect(page).toHaveURL(/agent=a1/)
  await expect(page.getByRole('complementary', { name: 'Transcript' }).getByText('sub task')).toBeVisible()
})
