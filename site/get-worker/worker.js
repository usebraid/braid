// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Serves the audited install script at https://get.usebraid.dev/ as text/plain, proxied (never redirected), so that
// `curl -fsSL https://get.usebraid.dev | sh` works. Nothing from the upstream is forwarded except a 200 body.
const DEFAULT_UPSTREAM = 'https://usebraid.dev/install.sh'
const MAX_BYTES = 256 * 1024

const text = (status, body, extra = {}) =>
  new Response(body, { status, headers: { 'content-type': 'text/plain; charset=utf-8', ...extra } })

// Reads the body up to MAX_BYTES; null if it is larger. shortcut: buffers up to 256 KiB, upgrade if the script outgrows that.
async function readCapped(res) {
  const declared = Number(res.headers.get('content-length'))
  if (declared > MAX_BYTES || !res.body) return declared > MAX_BYTES ? null : new Uint8Array()
  const chunks = []
  let size = 0
  const reader = res.body.getReader()
  for (let r = await reader.read(); !r.done; r = await reader.read()) {
    size += r.value.byteLength
    if (size > MAX_BYTES) {
      await reader.cancel()
      return null
    }
    chunks.push(r.value)
  }
  const out = new Uint8Array(size)
  let at = 0
  for (const c of chunks) {
    out.set(c, at)
    at += c.byteLength
  }
  return out
}

export default {
  async fetch(request, env) {
    if (request.method !== 'GET' && request.method !== 'HEAD')
      return text(405, 'method not allowed', { allow: 'GET, HEAD' })
    if (new URL(request.url).pathname !== '/') return text(404, 'not found')

    const upstream = env?.UPSTREAM_URL || DEFAULT_UPSTREAM
    let url
    try {
      url = new URL(upstream)
    } catch {
      return text(500, 'misconfigured')
    }
    if (url.protocol !== 'https:') return text(500, 'misconfigured')

    const unavailable = () => text(502, 'install script unavailable')
    let res
    try {
      res = await fetch(url.href, { redirect: 'error', headers: { accept: 'text/plain' } })
    } catch {
      return unavailable()
    }
    if (res.status !== 200) {
      await res.body?.cancel().catch(() => {})
      return unavailable()
    }
    const body = await readCapped(res).catch(() => null)
    if (!body) return unavailable()
    return new Response(request.method === 'HEAD' ? null : body, {
      status: 200,
      headers: {
        'content-type': 'text/plain; charset=utf-8',
        'cache-control': 'public, max-age=300',
        'x-content-type-options': 'nosniff',
      },
    })
  },
}
