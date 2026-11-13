// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Regenerates site/assets/shots/*.png from the synthetic fixtures of the web e2e harness (e2e/web), never from real sessions.
// Needs BRAID_TEST_DATABASE_URL (Postgres superuser URL), Go, and `cd web && npm ci && npx playwright install chromium`.
// Run: node site/scripts/screenshots.mjs
import { execFileSync, spawn } from 'node:child_process'
import { createRequire } from 'node:module'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')
const outDir = path.join(root, 'site/assets/shots')
const { chromium } = createRequire(path.join(root, 'web/package.json'))('@playwright/test')
const base = 'http://localhost:4173'
const control = 'http://localhost:4174'
const range = 'from=2026-10-01T00:00:00Z&to=2030-01-01T00:00:00Z' // the fixtures date from 2026-11-01
const pages = [
  ['board', `/?${range}`],
  ['session', `/sessions/g1?${range}`],
  ['failures', `/failures?${range}`],
]

if (!process.env.BRAID_TEST_DATABASE_URL) throw new Error('BRAID_TEST_DATABASE_URL is required')
execFileSync('go', ['test', '-c', '-tags', 'webe2e', '-o', 'web/.e2e/harness', './e2e/web'], {
  cwd: root,
  stdio: 'inherit',
})
const harness = spawn(path.join(root, 'web/.e2e/harness'), ['-test.run', '^TestServe$', '-test.timeout', '0'], {
  cwd: path.join(root, 'web'),
  stdio: 'inherit',
})
try {
  for (let i = 0; ; i++) {
    const ok = await fetch(`${base}/healthz`).then(
      (r) => r.ok,
      () => false,
    )
    if (ok) break
    if (i > 120) throw new Error('harness did not become healthy')
    await new Promise((r) => setTimeout(r, 1000))
  }
  await fetch(`${control}/user?sub=alice&admin=1`, { method: 'POST' })
  const browser = await chromium.launch()
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 }, colorScheme: 'dark', baseURL: base })
  await page.goto('/')
  await page.getByRole('main').waitFor()
  for (const [name, url] of pages) {
    await page.goto(url)
    await page.getByRole('main').waitFor()
    await page.waitForLoadState('networkidle')
    await page.screenshot({ path: path.join(outDir, `${name}.png`) })
    console.log(`wrote ${name}.png`)
  }
  await browser.close()
} finally {
  harness.kill()
}
