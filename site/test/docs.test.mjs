// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import assert from 'node:assert/strict'
import { readdirSync, readFileSync } from 'node:fs'
import path from 'node:path'
import { describe, test } from 'node:test'
import { fileURLToPath } from 'node:url'

const site = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const docsDir = path.join(site, 'docs')
const pages = ['quick-start', 'daemon', 'band', 'oidc-providers', 'reverse-proxy', 'external-postgres', 'operations']
const read = (p) => readFileSync(p, 'utf8')
const doc = (n) => read(path.join(docsDir, `${n}.md`))

describe('docs', () => {
  test('the six pages exist with order 1..6, a group and a description', () => {
    assert.deepEqual(readdirSync(docsDir).sort(), pages.map((p) => `${p}.md`).sort())
    pages.forEach((p, i) => {
      assert.match(doc(p), new RegExp(`^---\\ntitle: .+\\norder: ${i + 1}\\ngroup: .+\\ndescription: .+\\n---\\n`), p)
    })
  })

  test('external-postgres names no provider and has one BYPASSRLS note', () => {
    const t = doc('external-postgres')
    assert.doesNotMatch(t, /\b(RDS|Aurora|Cloud SQL|Supabase|Neon|Azure|Heroku|DigitalOcean|Crunchy)\b/i)
    const notes = t.split(/\n\n/).filter((p) => /BYPASSRLS/.test(p) && !/^(<!--|```|CREATE )/.test(p))
    assert.equal(notes.length, 1)
  })

  test('the compose override in the docs is the file in deploy/', () => {
    const block = /```yaml\n([\s\S]*?)```/.exec(doc('external-postgres'))[1].trim()
    const file = read(path.join(site, '..', 'deploy', 'docker-compose.external-db.yml'))
      .replace(/^#.*\n/gm, '')
      .trim()
    assert.equal(block, file)
  })

  test('every BRAID_* variable mentioned exists in the server config or the daemon paths', () => {
    const cfg = read(path.join(site, '..', 'internal', 'server', 'config', 'config.go'))
    const daemon = read(path.join(site, '..', 'internal', 'daemon', 'paths', 'paths.go'))
    const known = new Set([...cfg.match(/BRAID_[A-Z_]+/g), ...daemon.match(/BRAID_[A-Z_]+/g)])
    // Compose-level variables of deploy/.env.example, not server settings.
    const compose = new Set(['BRAID_DB_OWNER_PASSWORD', 'BRAID_DB_APP_PASSWORD'])
    for (const p of pages) {
      for (const v of doc(p).match(/BRAID_[A-Z_]+/g) ?? []) {
        assert.ok(known.has(v) || compose.has(v), `${p}: ${v}`)
      }
    }
  })

  test('no TODO', () => {
    for (const p of pages) assert.doesNotMatch(doc(p), /TODO/, p)
  })

  test('daemon has a Windows heading', () => assert.match(doc('daemon'), /^## Windows$/m))
})
