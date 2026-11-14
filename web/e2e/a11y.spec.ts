// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { loginAs, RANGE } from './helpers'

const pages = [
  '/',
  '/timeline',
  '/timeline?view=list',
  '/live',
  '/failures',
  '/search',
  '/devices',
  '/audit',
  '/admin',
  '/settings',
  '/sessions/g1',
  '/sessions/big1',
  '/sessions/big1?view=canvas',
  '/sessions/big1?view=list',
]

for (const theme of ['dark', 'light'] as const) {
  test(`no axe violations in ${theme} theme`, async ({ page, request }) => {
    await loginAs(page, request, 'alice')
    for (const p of pages) {
      await page.goto(`${p}${p.includes('?') ? '&' : '?'}${RANGE}`)
      await page.getByRole('main').waitFor()
      await page.evaluate((t) => document.documentElement.setAttribute('data-theme', t), theme)
      const res = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()
      expect(
        res.violations.map((v) => `${p}: ${v.id} ${v.nodes[0]?.target}`),
        p,
      ).toEqual([])
    }
  })
}

test('keyboard: skip link, focus ring and canvas agent list', async ({ page, request }) => {
  await loginAs(page, request, 'alice')
  await page.goto(`/sessions/g1?${RANGE}&view=list`)
  await page.getByRole('main').waitFor() // Tab before the app has rendered goes nowhere
  await page.keyboard.press('Tab')
  await expect(page.getByRole('link', { name: 'Skip to content' })).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(page.getByRole('main')).toBeFocused()
  const list = page.getByRole('button', { name: /d3|nested/i }).first()
  await list.focus()
  const outline = await list.evaluate((el) => getComputedStyle(el).outlineStyle)
  expect(outline).not.toBe('none')
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/agent=a3/)
})

test('charts are reachable by keyboard and reduced motion removes transitions', async ({ page, request }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await loginAs(page, request, 'alice')
  await page.goto(`/?${RANGE}`)
  await page.getByRole('group', { name: /Agent time, last \d+ days/ }).focus()
  await page.keyboard.press('Home')
  await expect(page.getByRole('status').filter({ hasText: /Oct 1/ })).toBeVisible()
  const dur = await page
    .getByRole('main')
    .evaluate((el) => getComputedStyle(el.querySelector('.card') ?? el).transitionDuration)
  expect(dur === '0s' || dur === '').toBe(true)
})

test('keyboard: graph rows are reachable with Tab', async ({ page, request }) => {
  await loginAs(page, request, 'alice')
  await page.goto(`/sessions/g1?${RANGE}`)
  const toggle = page.getByRole('button', { name: /^Subagent Explore: d1/ })
  await toggle.waitFor()
  for (let i = 0; i < 40 && !(await toggle.evaluate((el) => el === document.activeElement)); i++)
    await page.keyboard.press('Tab')
  await expect(toggle).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(toggle).toHaveAttribute('aria-expanded', 'false')
})
