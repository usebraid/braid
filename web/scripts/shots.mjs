// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Screenshots every view in dark and light, at 1280×800 and 390×844, from the synthetic e2e fixtures (e2e/web), for
// design review. Needs BRAID_TEST_DATABASE_URL, Go, a built web/dist and `npx playwright install chromium`.
// Run: node web/scripts/shots.mjs <out-dir>   (builds and starts its own harness)
import { execFileSync, spawn } from 'node:child_process'
import { mkdirSync } from 'node:fs'
import { createRequire } from 'node:module'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')
const outDir = path.resolve(process.argv[2] ?? 'shots')
const { chromium } = createRequire(path.join(root, 'web/package.json'))('@playwright/test')
const base = 'http://localhost:4173'
const range = 'from=2026-10-17T00:00:00Z&to=2026-11-16T00:00:00Z' // 30 days around the fixtures of 2026-11-01 and today
const views = [
  ['overview', `/?${range}`],
  ['overview-1day', `/?${range}&project=%2Fwork%2Falpha`],
  ['timeline', `/timeline?${range}`],
  ['live', '/live'],
  ['failures', `/failures?${range}`],
  ['search', `/search?${range}&q=prompt`],
  ['devices', '/devices'],
  ['audit', '/audit'],
  ['admin', '/admin'],
  ['settings', '/settings'],
  ['session', `/sessions/g1?${range}`],
  ['session-transcript', `/sessions/g1?${range}&agent=a1`],
  ['session-big', `/sessions/big1?${range}`],
  ['session-big-list', `/sessions/big1?${range}&view=list`],
  ['notfound', '/nope'],
]
const healthy = () =>
  fetch(`${base}/healthz`).then(
    (r) => r.ok,
    () => false,
  )

if (await healthy()) throw new Error('stop the harness on :4173 first; this script builds its own from web/dist')
if (!process.env.BRAID_TEST_DATABASE_URL) throw new Error('BRAID_TEST_DATABASE_URL is required')
execFileSync('go', ['test', '-c', '-tags', 'webe2e', '-o', 'web/.e2e/harness', './e2e/web'], {
  cwd: root,
  stdio: 'inherit',
})
const harness = spawn(path.join(root, 'web/.e2e/harness'), ['-test.run', '^TestServe$', '-test.timeout', '0'], {
  cwd: path.join(root, 'web'),
  stdio: 'inherit',
})
for (let i = 0; !(await healthy()); i++) {
  if (i > 180) throw new Error('harness did not become healthy')
  await new Promise((r) => setTimeout(r, 1000))
}
try {
  await fetch('http://localhost:4174/user?sub=alice&admin=1', { method: 'POST' })
  const browser = await chromium.launch()
  for (const theme of ['dark', 'light']) {
    for (const [vp, size] of [
      ['desktop', { width: 1280, height: 800 }],
      ['mobile', { width: 390, height: 844 }],
    ]) {
      const ctx = await browser.newContext({ viewport: size, colorScheme: theme, baseURL: base })
      await ctx.addInitScript((t) => localStorage.setItem('braid-theme', t), theme)
      const page = await ctx.newPage()
      await page.goto('/')
      await page.getByRole('main').waitFor()
      const dir = path.join(outDir, `${theme}-${vp}`)
      mkdirSync(dir, { recursive: true })
      for (const [name, url] of views) {
        await page.goto(url)
        await page.getByRole('main').waitFor()
        await page.waitForLoadState('networkidle')
        await page.waitForTimeout(400)
        await page.screenshot({ path: path.join(dir, `${name}.png`), fullPage: !name.startsWith('session') })
      }
      if (vp === 'mobile') {
        const menu = page.getByRole('button', { name: /menu/i })
        if (await menu.count()) {
          await menu.click()
          await page.screenshot({ path: path.join(dir, 'nav-open.png') })
        }
      } else {
        await page.goto('/devices')
        await page
          .getByRole('button', { name: /^Revoke / })
          .first()
          .click()
        await page.screenshot({ path: path.join(dir, 'dialog.png') })
      }
      await ctx.close()
    }
  }
  await browser.close()
  console.log(`wrote ${outDir}`)
} finally {
  harness.kill()
}
