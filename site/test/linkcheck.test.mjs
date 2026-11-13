// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { after, describe, test } from 'node:test'
import { fileURLToPath } from 'node:url'

const site = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const tmp = mkdtempSync(path.join(tmpdir(), 'braid-linkcheck-'))
after(() => rmSync(tmp, { recursive: true, force: true }))

function tree(name, files) {
  const dir = path.join(tmp, name)
  for (const [f, body] of Object.entries(files)) {
    mkdirSync(path.dirname(path.join(dir, f)), { recursive: true })
    writeFileSync(path.join(dir, f), body)
  }
  return dir
}
const run = (dir) => spawnSync('node', ['linkcheck.mjs', dir], { cwd: site, encoding: 'utf8' })

const good = {
  'index.html':
    '<a href="/docs/">d</a><a href="/docs/a/#x">a</a><a href="#top" id="top">t</a><link href="/style.css"><a href="mailto:a@b.c">m</a><a href="https://example.com">e</a>',
  'style.css': '',
  'docs/index.html': '<a href="a/">a</a><a href="../">home</a>',
  'docs/a/index.html': '<h2 id="x">x</h2>',
}

describe('linkcheck', () => {
  test('a good tree passes', () => {
    const r = run(tree('good', good))
    assert.equal(r.status, 0, r.stderr)
  })

  test('a broken file link fails and names it', () => {
    const r = run(tree('file', { ...good, 'index.html': '<a href="/docs/missing.html">x</a>' }))
    assert.equal(r.status, 1)
    assert.match(r.stderr, /index\.html -> \/docs\/missing\.html/)
  })

  test('a broken anchor fails and names it', () => {
    const r = run(tree('anchor', { ...good, 'index.html': '<a href="/docs/a/#nope">x</a>' }))
    assert.equal(r.status, 1)
    assert.match(r.stderr, /\/docs\/a\/#nope/)
  })

  test('a directory without index.html fails', () => {
    const r = run(tree('noindex', { ...good, 'index.html': '<a href="/empty/">x</a>', 'empty/page.html': '' }))
    assert.equal(r.status, 1)
    assert.match(r.stderr, /index\.html -> \/empty\//)
  })
})
