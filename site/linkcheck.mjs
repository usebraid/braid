// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Usage: node linkcheck.mjs <dist-dir> [--external]
// Checks that every relative or root-relative link and #fragment in the built HTML resolves. --external also
// requests each distinct https:// link (network; not run in CI).
import { readdir, readFile, stat } from 'node:fs/promises'
import path from 'node:path'

const args = process.argv.slice(2)
const external = args.includes('--external')
const dist = args.find((a) => !a.startsWith('--'))
if (!dist) {
  console.error('usage: node linkcheck.mjs <dist-dir> [--external]')
  process.exit(2)
}
const root = path.resolve(dist)

async function htmlFiles(dir) {
  const out = []
  for (const e of await readdir(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name)
    if (e.isDirectory()) out.push(...(await htmlFiles(p)))
    else if (e.name.endsWith('.html')) out.push(p)
  }
  return out
}

const isFile = (p) =>
  stat(p).then(
    (s) => s.isFile(),
    () => false,
  )

// Regex extraction is enough for our own generated pages (double-quoted attributes).
const attrs = (html, name) => [...html.matchAll(new RegExp(`\\s${name}="([^"]*)"`, 'g'))].map((m) => m[1])
const decode = (s) => s.replace(/&amp;/g, '&')

const pages = new Map() // file -> { ids:Set, links:[] }
for (const f of await htmlFiles(root)) {
  const html = await readFile(f, 'utf8')
  pages.set(f, { ids: new Set(attrs(html, 'id')), links: [...attrs(html, 'href'), ...attrs(html, 'src')].map(decode) })
}

// A URL path resolves to a file, or to dir/index.html.
async function resolveTarget(from, p) {
  const base = p.startsWith('/') ? path.join(root, p) : path.resolve(path.dirname(from), p)
  if (base !== root && !base.startsWith(root + path.sep)) return null
  for (const c of [base, path.join(base, 'index.html')]) {
    if (c === base && p.endsWith('/')) continue
    if (await isFile(c)) return c
  }
  return null
}

const failures = []
const externals = new Set()
for (const [file, { links }] of pages) {
  const rel = path.relative(root, file)
  for (const link of links) {
    if (/^(mailto|tel|data|javascript):/i.test(link) || link === '') continue
    if (/^https:\/\//i.test(link)) {
      externals.add(link)
      continue
    }
    if (/^([a-z][a-z0-9+.-]*:|\/\/)/i.test(link)) continue // http:, protocol-relative: not checked
    const [, pathPart, frag] = /^([^#?]*)[^#]*(?:#(.*))?$/.exec(link)
    const target = pathPart === '' ? file : await resolveTarget(file, pathPart)
    if (!target) {
      failures.push(`${rel} -> ${link}`)
      continue
    }
    if (frag) {
      const ids = pages.get(target)?.ids
      if (!ids?.has(decodeURIComponent(frag))) failures.push(`${rel} -> ${link} (no such anchor)`)
    }
  }
}

async function head(url) {
  for (const method of ['HEAD', 'GET']) {
    try {
      const r = await fetch(url, { method, redirect: 'follow', signal: AbortSignal.timeout(10_000) })
      if (r.ok || method === 'GET') return r.ok ? null : `${r.status}`
    } catch (e) {
      if (method === 'GET') return e.message
    }
  }
  return null
}

if (external) {
  const queue = [...externals]
  await Promise.all(
    Array.from({ length: 4 }, async () => {
      for (let url = queue.shift(); url; url = queue.shift()) {
        const err = await head(url)
        if (err) failures.push(`${url} -> ${err}`)
      }
    }),
  )
}

if (failures.length) {
  console.error(`linkcheck: ${failures.length} broken link(s)\n${failures.join('\n')}`)
  process.exit(1)
}
console.log(`linkcheck: ${pages.size} pages ok${external ? `, ${externals.size} external links ok` : ''}`)
