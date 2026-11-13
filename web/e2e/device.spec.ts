// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { expect, test } from '@playwright/test'
import { loginAs } from './helpers'

test('a real form submit approves a device and the token can be polled', async ({ page, request }) => {
  await loginAs(page, request, 'alice')
  const code = await request.post('/v1/device/code', {
    data: { hostname: 'e2e-host', os: 'linux', arch: 'amd64', os_user: 'e2e', daemon_version: '0.0.0' },
  })
  expect(code.ok()).toBeTruthy()
  const dc = await code.json()
  await page.goto(`/device?user_code=${dc.user_code}`)
  await page.getByRole('button', { name: 'Approve' }).click()
  await expect(page.getByRole('heading')).not.toContainText(/forbidden|origin/i)
  const grant = 'urn:ietf:params:oauth:grant-type:device_code'
  await expect(async () => {
    const t = await request.post('/v1/device/token', { data: { grant_type: grant, device_code: dc.device_code } })
    expect(t.status()).toBe(200)
    expect((await t.json()).access_token).toMatch(/^braid_dev_/)
  }).toPass({ timeout: 10_000 })
})
