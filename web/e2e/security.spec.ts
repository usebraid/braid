// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { expect, test } from '@playwright/test'
import { loginAs } from './helpers'

test('the shell carries the strict CSP and has no inline script', async ({ page, request }) => {
  await loginAs(page, request, 'alice')
  const resp = await page.request.get('/')
  const csp = resp.headers()['content-security-policy']
  expect(csp).toContain("script-src 'self'")
  expect(csp).toContain("connect-src 'self'")
  expect(csp).not.toContain('unsafe-inline')
  const html = await resp.text()
  expect(html).not.toMatch(/<script(?![^>]*\bsrc=)[^>]*>/)
})

test('an anonymous request gets no app shell', async ({ request }) => {
  const r = await request.get('/', { maxRedirects: 0 })
  expect(r.status()).toBe(302)
  expect(r.headers().location).toContain('/auth/login')
})
