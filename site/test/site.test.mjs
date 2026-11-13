// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { accessSync, constants, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { after, describe, test } from 'node:test'
import { fileURLToPath } from 'node:url'

const site = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const tmp = mkdtempSync(path.join(tmpdir(), 'braid-site-'))
after(() => import('node:fs').then((fs) => fs.rmSync(tmp, { recursive: true, force: true })))

function build(name, docs) {
  const outDir = path.join(tmp, name)
  const env = { ...process.env, OUT_DIR: outDir }
  if (docs) {
    const d = path.join(tmp, `${name}-docs`)
    mkdirSync(d)
    for (const [f, body] of Object.entries(docs)) writeFileSync(path.join(d, f), body)
    env.DOCS_DIR = d
  }
  const r = spawnSync('node', ['build.mjs'], { cwd: site, env, encoding: 'utf8' })
  return { outDir, r, read: (f) => readFileSync(path.join(outDir, f), 'utf8') }
}

const main = build('main')
const html = main.r.status === 0 ? main.read('index.html') : ''

describe('landing page', () => {
  test('builds', () => assert.equal(main.r.status, 0, main.r.stderr))

  test('has the hero sentence and every section id', () => {
    assert.ok(html.includes('Every Claude Code session, every device, one stream'))
    for (const id of ['features', 'how-it-works', 'install', 'self-hosting', 'credits', 'license']) {
      assert.ok(html.includes(`id="${id}"`), id)
    }
  })

  test('makes no third-party requests and has no script', () => {
    assert.ok(!/<script/i.test(html))
    const css = readFileSync(path.join(main.outDir, 'style.css'), 'utf8')
    assert.ok(!/@import|url\(\s*['"]?https?:/i.test(css))
    const allowed =
      /^https:\/\/(usebraid\.dev|github\.com\/(usebraid|simple10\/agents-observe|DietrichGebert\/ponytail))(\/|$)/
    for (const m of html.matchAll(/<(\w+)\b[^>]*?\s(src|href|action)="(https?:\/\/[^"]+)"/g)) {
      const [, tag, attr, url] = m
      assert.ok(tag === 'a' && attr === 'href' && allowed.test(url), `${tag} ${attr} ${url}`)
    }
    assert.ok(!/url\(\s*['"]?https?:/i.test(html))
  })

  test('hero holds exactly the one-liner', () => {
    const hero = html.slice(html.indexOf('class="hero"'), html.indexOf('id="screenshots"'))
    assert.equal(hero.split('curl -fsSL https://get.usebraid.dev | sh').length - 1, 1)
    assert.equal(hero.split('curl').length - 1, 1)
  })

  test('ships install.sh, CNAME and .nojekyll', () => {
    assert.equal(main.read('CNAME'), 'usebraid.dev')
    main.read('.nojekyll')
    accessSync(path.join(main.outDir, 'install.sh'), constants.X_OK)
  })
})

describe('docs', () => {
  test('renders nav, anchors and escapes raw HTML', () => {
    const b = build('docs-ok', {
      'daemon.md':
        '---\ntitle: Daemon\norder: 1\n---\n# Daemon\n\n## Windows\n\n<script>alert(1)</script>\n\n<!-- sql:ok -->\n\n[x](javascript:alert(1))\n',
    })
    assert.equal(b.r.status, 0, b.r.stderr)
    const page = b.read('docs/daemon/index.html')
    assert.ok(page.includes('<a href="/docs/daemon/">Daemon</a>'))
    assert.ok(page.includes('id="windows"'))
    assert.ok(!page.includes('<script>alert'))
    assert.ok(page.includes('&lt;script&gt;'))
    assert.ok(page.includes('<!-- sql:ok -->'))
    assert.ok(!page.includes('javascript:'))
  })

  test('fails on a missing title, a missing order and a duplicate order', () => {
    assert.notEqual(build('bad-title', { 'a.md': '---\norder: 1\n---\nx' }).r.status, 0)
    assert.notEqual(build('bad-order', { 'a.md': '---\ntitle: A\n---\nx' }).r.status, 0)
    const dup = { 'a.md': '---\ntitle: A\norder: 1\n---\nx', 'b.md': '---\ntitle: B\norder: 1\n---\nx' }
    assert.notEqual(build('dup', dup).r.status, 0)
  })
})
