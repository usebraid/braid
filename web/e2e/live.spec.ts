// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { expect, test } from '@playwright/test'
import { CONTROL, loginAs } from './helpers'

test('a pushed update appears without reload and a dropped socket shows the paused banner', async ({
  page,
  request,
  context,
}) => {
  test.setTimeout(240_000) // the client notices a silent socket after 70 s
  await loginAs(page, request, 'alice')
  await page.goto('/live')
  const card = page.getByRole('link', { name: /Live fixture/ }).first()
  await expect(card).toBeVisible()
  await expect(page.getByText(/Receiving live updates/)).toBeVisible()
  const before = await page.getByTestId('live-last-activity').first().textContent()
  await page.waitForTimeout(1500)
  await request.post(`${CONTROL}/append?session=l1`)
  await expect(page.getByTestId('live-last-activity').first()).not.toHaveText(before ?? '')
  await context.setOffline(true)
  await expect(page.getByRole('status').filter({ hasText: /paused/i })).toBeVisible({ timeout: 90_000 })
  await context.setOffline(false)
  await expect(page.getByText(/Receiving live updates/)).toBeVisible({ timeout: 60_000 })
})

test('the socket works same-origin and is refused for a foreign Origin even with a valid cookie', async ({
  page,
  request,
}) => {
  await loginAs(page, request, 'alice')
  const opened = await page.evaluate(
    () =>
      new Promise<boolean>((resolve) => {
        const ws = new WebSocket(`ws://${location.host}/api/v1/live`)
        ws.onopen = () => resolve(true)
        ws.onerror = () => resolve(false)
      }),
  )
  expect(opened).toBe(true)
  const r = await page.context().request.get('http://localhost:4173/api/v1/live', {
    headers: {
      Origin: 'http://evil.example',
      Connection: 'Upgrade',
      Upgrade: 'websocket',
      'Sec-WebSocket-Version': '13',
      'Sec-WebSocket-Key': 'dGhlIHNhbXBsZSBub25jZQ==', // gitleaks:allow RFC 6455 example nonce
    },
  })
  expect(r.status()).toBe(403)
})
