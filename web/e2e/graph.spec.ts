// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { expect, test } from '@playwright/test'
import { CONTROL, loginAs, RANGE } from './helpers'

test.describe('graph views as alice', () => {
  test.beforeEach(async ({ page, request }) => {
    await loginAs(page, request, 'alice')
  })

  test('timeline graph shows the resumed session twice with the device names', async ({ page }) => {
    await page.goto(`/timeline?${RANGE}`)
    const rows = page.getByRole('link', { name: /Resumed later/ })
    await expect(rows).toHaveCount(2)
    const started = rows.filter({ hasText: /· started/ })
    const resumed = rows.filter({ hasText: /· resumed/ })
    await expect(started).toContainText('laptop')
    await expect(resumed).toContainText('desktop')
    await expect(page.getByRole('link', { name: 'Bob private' })).toHaveCount(0)
  })

  test('session graph: branch and merge rows, collapsing, deep link into a subagent', async ({ page }) => {
    await page.goto(`/sessions/g1?${RANGE}`)
    const chat = page.getByRole('region', { name: 'Session chat' })
    await expect(chat.locator('.grow--branch').first()).toBeVisible()
    await expect(chat.locator('.grow--merge').first()).toBeVisible()
    await expect(chat.getByText('sub task')).toBeVisible()

    await chat.getByRole('button', { name: 'Collapse subagents' }).click()
    await expect(chat.getByText('sub task')).toHaveCount(0)
    const branch = chat.getByRole('button', { name: /^Subagent Explore: d1/ })
    await expect(branch).toHaveAttribute('aria-expanded', 'false')
    await chat.getByRole('button', { name: 'Expand subagents' }).click()
    await expect(branch).toHaveAttribute('aria-expanded', 'true')
    await branch.click()
    await expect(chat.getByText('sub task')).toHaveCount(0)

    // deep link to a line of agent a1 opens its collapsed branch and focuses the row
    const r = await page.request.get(`/api/v1/stats/sessions/g1/chat?limit=500`)
    const lines: { id: number; agent_id: string; line: { type?: string } }[] = (await r.json()).lines
    const id = lines.find((l) => l.agent_id === 'a1' && l.line.type === 'user')?.id
    expect(id).toBeTruthy()
    await page.goto(`/sessions/g1?${RANGE}&line=${id}`)
    const target = page.locator('li[aria-current="true"]')
    await expect(target).toContainText('sub task')
    await expect(target).toBeFocused()
  })

  test('live: an appended line shows as a new row at the bottom', async ({ page, request }) => {
    await page.goto('/sessions/l1')
    const rows = page.locator('ol.grows > li.grow')
    await expect(rows.first()).toBeVisible()
    const before = await rows.count()
    await request.post(`${CONTROL}/append?session=l1`)
    await expect.poll(() => rows.count(), { timeout: 15_000 }).toBeGreaterThan(before)
    await expect(rows.last()).toContainText('live reply')
  })

  test('scrolling the 102-subagent session to the end causes no long task over 50 ms', async ({ page }) => {
    await page.goto(`/sessions/big1?${RANGE}`)
    const rows = page.locator('ol.grows > li.grow')
    await expect(rows.first()).toBeVisible()
    // 4x CPU throttling stands in for a 2 vCPU CI runner, so local runs catch what only CI showed
    await (await page.context().newCDPSession(page)).send('Emulation.setCPUThrottlingRate', { rate: 4 })
    await page.evaluate(() => {
      const w = window as unknown as { __long: number[] }
      w.__long = []
      new PerformanceObserver((l) => {
        for (const e of l.getEntries()) w.__long.push(e.duration)
      }).observe({ type: 'longtask', buffered: false })
    })
    // The end check is plain DOM: a getByRole count walks every row's accessibility tree in the page's main thread and
    // would itself show up as a long task.
    const atEnd = () =>
      page.evaluate(
        () =>
          window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 2 &&
          ![...document.querySelectorAll('section.graph > button')].some((b) => b.textContent === 'Load more'),
      )
    for (let i = 0; i < 400 && !(await atEnd()); i++) {
      await page.mouse.wheel(0, 4000)
      await page.waitForTimeout(30)
    }
    expect(await atEnd()).toBe(true)
    const long = await page.evaluate(() => (window as unknown as { __long: number[] }).__long)
    expect(long.filter((d) => d > 50)).toEqual([])
  })
})
