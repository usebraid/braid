// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Builds the static site into OUT_DIR (default dist/). DOCS_DIR overrides the Markdown source (default docs/).
import { chmod, cp, mkdir, readdir, readFile, rm, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { Marked } from 'marked'

const here = path.dirname(fileURLToPath(import.meta.url))
const out = path.resolve(process.env.OUT_DIR || path.join(here, 'dist'))
const docsDir = process.env.DOCS_DIR ? path.resolve(process.env.DOCS_DIR) : path.join(here, 'docs')
const shots = [
  ['board', 'The board: your sessions, cost and tokens across all devices.'],
  ['session', 'A session as a canvas of the main session and its subagents.'],
  ['failures', 'Failures: what went wrong, how often, and how much time it cost.'],
]

const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
const fill = (tpl, vars) => tpl.replace(/\{\{(\w+)\}\}/g, (_, k) => vars[k] ?? '')
const exists = (p) =>
  readFile(p).then(
    () => true,
    () => false,
  )

// Reads `---\ntitle: ...\norder: n\n---` and returns the fields plus the body.
function frontMatter(src, name) {
  const m = /^---\r?\n([\s\S]*?)\r?\n---\r?\n?/.exec(src)
  const meta = {}
  if (m) {
    for (const line of m[1].split(/\r?\n/)) {
      const i = line.indexOf(':')
      if (i > 0) meta[line.slice(0, i).trim()] = line.slice(i + 1).trim()
    }
  }
  if (!meta.title) throw new Error(`${name}: front matter needs a title`)
  if (!/^\d+$/.test(meta.order ?? '')) throw new Error(`${name}: front matter needs a numeric order`)
  return {
    title: meta.title,
    order: Number(meta.order),
    group: meta.group || 'Docs',
    description: meta.description || '',
    body: src.slice(m[0].length),
  }
}

const slug = (s) =>
  s
    .toLowerCase()
    .replace(/<[^>]*>/g, '')
    .replace(/[^\p{L}\p{N}\s-]/gu, '')
    .trim()
    .replace(/\s+/g, '-')

function renderer() {
  const seen = new Map()
  const toc = []
  const safeUrl = (h) => /^(https?:|mailto:|#|\/|\.{0,2}\/|[^:]*$)/i.test(h ?? '')
  const marked = new Marked({
    renderer: {
      // No raw HTML: escape it, except whole comments (the sql markers), which stay inert comments.
      html({ text }) {
        return /^<!--((?!-->)[\s\S])*-->\s*$/.test(text) ? text : esc(text)
      },
      heading({ tokens, depth }) {
        const text = this.parser.parseInline(tokens)
        const base = slug(text) || 'section'
        const n = seen.get(base) ?? 0
        seen.set(base, n + 1)
        const id = n ? `${base}-${n}` : base
        if (depth === 2) toc.push({ id, text })
        return `<h${depth} id="${id}">${text}</h${depth}>\n`
      },
      link({ href, title, tokens }) {
        const text = this.parser.parseInline(tokens)
        if (!safeUrl(href)) return text
        return `<a href="${esc(href)}"${title ? ` title="${esc(title)}"` : ''}>${text}</a>`
      },
      image({ href, text }) {
        return safeUrl(href) ? `<img src="${esc(href)}" alt="${esc(text)}">` : esc(text)
      },
    },
  })
  return { marked, toc }
}

// The hero picture: three strands (one per device) braid with a shrinking amplitude into one stream. Drawn in a
// 1000x220 box and stretched to the width (preserveAspectRatio none, non-scaling strokes), so it fits any screen.
function braidSvg() {
  const end = 760
  const strands = [0, (2 * Math.PI) / 3, (4 * Math.PI) / 3].map((phase, i) => {
    const pts = []
    for (let x = 0; x <= end; x += 5) {
      const t = x / end
      const amp = 62 * (1 - t) ** 1.4
      pts.push(`${x} ${(110 - amp * Math.sin(2 * Math.PI * 2.2 * t + phase)).toFixed(1)}`)
    }
    return `<path class="s s${i + 1}" d="M${pts.join(' L')}"/>`
  })
  // Each strand twice: a background-coloured halo under it, so crossings read as over and under.
  const halo = strands.map((p) => p.replace('class="s ', 'class="halo '))
  const woven = strands.flatMap((p, i) => [halo[i], p])
  return `<svg class="braid" viewBox="0 0 1000 220" preserveAspectRatio="none" aria-hidden="true" focusable="false">
${woven.join('\n')}
<path class="s s0" d="M${end} 110 L992 110"/>
</svg>`
}

async function loadDocs() {
  let files
  try {
    files = (await readdir(docsDir)).filter((f) => f.endsWith('.md')).sort()
  } catch (e) {
    if (e.code === 'ENOENT' && !process.env.DOCS_DIR) return []
    throw new Error(`cannot read docs dir ${docsDir}: ${e.message}`)
  }
  const docs = []
  for (const f of files) {
    const fm = frontMatter(await readFile(path.join(docsDir, f), 'utf8'), f)
    docs.push({ ...fm, slug: f.slice(0, -3), file: f })
  }
  const orders = new Set()
  for (const d of docs) {
    if (orders.has(d.order)) throw new Error(`${d.file}: duplicate order ${d.order}`)
    orders.add(d.order)
  }
  return docs.sort((a, b) => a.order - b.order)
}

async function main() {
  const docs = await loadDocs()
  await rm(out, { recursive: true, force: true })
  await mkdir(out, { recursive: true })
  await cp(path.join(here, 'style.css'), path.join(out, 'style.css'))
  await cp(path.join(here, 'assets'), path.join(out, 'assets'), { recursive: true })
  await cp(path.join(here, 'CNAME'), path.join(out, 'CNAME'))
  await cp(path.join(here, 'install.sh'), path.join(out, 'install.sh'))
  await chmod(path.join(out, 'install.sh'), 0o755)
  await writeFile(path.join(out, '.nojekyll'), '')

  const figures = []
  for (const [name, caption] of shots) {
    if (await exists(path.join(here, 'assets', 'shots', `${name}.png`))) {
      figures.push(
        `    <figure><img src="/assets/shots/${name}.png" alt="${esc(caption)}" loading="lazy"><figcaption>${esc(caption)}</figcaption></figure>`,
      )
    }
  }
  const index = await readFile(path.join(here, 'index.html'), 'utf8')
  await writeFile(path.join(out, 'index.html'), fill(index, { shots: figures.join('\n'), braid: braidSvg() }))

  const tpl = await readFile(path.join(here, 'docs.html'), 'utf8')
  const groups = [...new Set(docs.map((d) => d.group))]
  const nav = (current) =>
    groups
      .map((g) => {
        const items = docs
          .filter((d) => d.group === g)
          .map((d) => {
            const cur = d.slug === current ? ' aria-current="page"' : ''
            return `<li><a href="/docs/${d.slug}/"${cur}>${esc(d.title)}</a></li>`
          })
        return `<li class="group"><span>${esc(g)}</span><ul>${items.join('')}</ul></li>`
      })
      .join('\n')
  const page = async (dir, title, current, content, toc = '') => {
    await mkdir(path.join(out, 'docs', dir), { recursive: true })
    const html = fill(tpl, { title: esc(title), nav: nav(current), content, toc })
    await writeFile(path.join(out, 'docs', dir, 'index.html'), html)
  }
  for (const [i, d] of docs.entries()) {
    const { marked, toc } = renderer()
    const body = marked.parse(d.body)
    const link = (o, rel, label) =>
      o ? `<a rel="${rel}" href="/docs/${o.slug}/"><span>${label}</span>${esc(o.title)}</a>` : '<span></span>'
    const pager = `<nav class="pager" aria-label="Pages">${link(docs[i - 1], 'prev', 'Previous')}${link(docs[i + 1], 'next', 'Next')}</nav>`
    const edit = `<p class="edit"><a href="https://github.com/usebraid/braid/edit/main/site/docs/${d.file}">Edit this page on GitHub</a></p>`
    const side = toc.length
      ? `<nav class="toc" aria-label="On this page"><p>On this page</p><ul>${toc.map((h) => `<li><a href="#${h.id}">${h.text}</a></li>`).join('')}</ul></nav>`
      : ''
    await page(d.slug, d.title, d.slug, `${body}${edit}${pager}`, side)
  }
  const list = groups
    .map((g) => {
      const items = docs
        .filter((d) => d.group === g)
        .map((d) => `<li><a href="/docs/${d.slug}/">${esc(d.title)}</a><p>${esc(d.description)}</p></li>`)
      return `<h2>${esc(g)}</h2>\n<ul class="index">${items.join('')}</ul>`
    })
    .join('\n')
  const intro =
    '<p class="lead">Braid has two parts: a daemon on each device and a server you run. New here? Start with the quick start.</p>'
  await page('.', 'Documentation', '', `<h1>Documentation</h1>\n${intro}\n${list}`)
}

main().catch((e) => {
  console.error(`build: ${e.message}`)
  process.exit(1)
})
