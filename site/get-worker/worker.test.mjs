// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import path from 'node:path'
import { afterEach, describe, test } from 'node:test'
import { fileURLToPath } from 'node:url'
import worker from './worker.js'

const UPSTREAM = 'https://usebraid.dev/install.sh'
const realFetch = globalThis.fetch
afterEach(() => {
  globalThis.fetch = realFetch
})

// Stubs fetch; returns the recorded calls.
function stub(handler) {
  const calls = []
  globalThis.fetch = async (url, init) => {
    calls.push({ url, init })
    return handler(url, init)
  }
  return calls
}
const call = (method, p = '/', env = {}) => worker.fetch(new Request(`https://get.usebraid.dev${p}`, { method }), env)
const script = '#!/bin/sh\necho hi\n'
const ok = () => new Response(script, { status: 200 })

describe('get worker', () => {
  test('GET / proxies the script as text/plain, never a redirect', async () => {
    const calls = stub(ok)
    const r = await call('GET')
    assert.equal(r.status, 200)
    assert.equal(await r.text(), script)
    assert.match(r.headers.get('content-type'), /^text\/plain/)
    assert.equal(r.headers.get('x-content-type-options'), 'nosniff')
    assert.equal(r.headers.get('location'), null)
    assert.equal(r.headers.get('content-disposition'), null)
    assert.equal(calls.length, 1)
    assert.equal(calls[0].url, UPSTREAM)
    assert.equal(calls[0].init.redirect, 'error')
  })

  test('the query is ignored', async () => {
    stub(ok)
    assert.equal((await call('GET', '/?x=1')).status, 200)
  })

  test('HEAD / has the headers and an empty body', async () => {
    stub(ok)
    const r = await call('HEAD')
    assert.equal(r.status, 200)
    assert.equal(await r.text(), '')
    assert.match(r.headers.get('content-type'), /^text\/plain/)
    assert.equal(r.headers.get('x-content-type-options'), 'nosniff')
  })

  test('other paths are 404, other methods 405 with allow', async () => {
    stub(ok)
    assert.equal((await call('GET', '/install.sh')).status, 404)
    assert.equal((await call('GET', '/x')).status, 404)
    const r = await call('POST')
    assert.equal(r.status, 405)
    assert.equal(r.headers.get('allow'), 'GET, HEAD')
  })

  for (const [name, handler] of [
    ['404', () => new Response('SECRET upstream text', { status: 404 })],
    ['500', () => new Response('SECRET upstream text', { status: 500 })],
    [
      'network error',
      () => {
        throw new TypeError('SECRET upstream text')
      },
    ],
    [
      '301 (redirect: error throws)',
      () => {
        throw new TypeError('redirect mode is set to error')
      },
    ],
  ]) {
    test(`upstream ${name} gives a generic 502`, async () => {
      stub(handler)
      const r = await call('GET')
      assert.equal(r.status, 502)
      assert.equal(r.headers.get('location'), null)
      const body = await r.text()
      assert.equal(body, 'install script unavailable')
    })
  }

  test('an oversized upstream gives 502', async () => {
    stub(() => new Response('x'.repeat(257 * 1024), { status: 200 }))
    assert.equal((await call('GET')).status, 502)
    stub(() => new Response('x', { status: 200, headers: { 'content-length': String(257 * 1024) } }))
    assert.equal((await call('GET')).status, 502)
  })

  test('a non-https UPSTREAM_URL gives 500, an override is used', async () => {
    const calls = stub(ok)
    assert.equal((await call('GET', '/', { UPSTREAM_URL: 'http://usebraid.dev/install.sh' })).status, 500)
    assert.equal(calls.length, 0)
    assert.equal((await call('GET', '/', { UPSTREAM_URL: 'https://example.test/i.sh' })).status, 200)
    assert.equal(calls[0].url, 'https://example.test/i.sh')
  })

  test('serves the real install.sh byte for byte', async () => {
    const file = readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)), '..', 'install.sh'))
    stub(() => new Response(file, { status: 200 }))
    const r = await call('GET')
    assert.equal(r.status, 200)
    assert.deepEqual(Buffer.from(await r.arrayBuffer()), file)
  })
})
