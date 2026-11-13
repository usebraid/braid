// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors
// @vitest-environment node

import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { nextTheme } from './theme'

const css = readFileSync(new URL('./tokens.css', import.meta.url), 'utf8')
const block = (sel: string) => {
  const m = css.match(new RegExp(`${sel.replace(/[[\].=]/g, '\\$&')}\\s*\\{([^}]*)\\}`))
  if (!m) throw new Error(`no ${sel}`)
  return Object.fromEntries(
    [...(m[1] ?? '').matchAll(/--([\w-]+)\s*:\s*([^;]+);/g)].map((x) => [x[1] as string, (x[2] as string).trim()]),
  )
}
const lum = (hex: string) => {
  const c = [1, 3, 5]
    .map((i) => parseInt(hex.slice(i, i + 2), 16) / 255)
    .map((v) => (v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4))
  return 0.2126 * (c[0] as number) + 0.7152 * (c[1] as number) + 0.0722 * (c[2] as number)
}
const ratio = (a: string, b: string) => {
  const [x, y] = [lum(a), lum(b)].sort((p, q) => q - p) as [number, number]
  return (x + 0.05) / (y + 0.05)
}

describe('theme tokens', () => {
  for (const [name, vars] of [
    ['dark', block(':root')],
    ['light', block(':root[data-theme=light]')],
  ] as const) {
    it(`${name}: text tokens reach 4.5:1 on every surface`, () => {
      for (const fg of ['text', 'muted', 'faint', 'ok', 'bad', 'warn']) {
        for (const bg of ['bg', 'panel', 'card-solid', 'track']) {
          expect(ratio(vars[fg] as string, vars[bg] as string), `${name} ${fg} on ${bg}`).toBeGreaterThanOrEqual(4.5)
        }
      }
      expect(ratio(vars.focus as string, vars.bg as string), `${name} focus ring`).toBeGreaterThanOrEqual(3)
    })
  }
  it('cycles dark -> light -> system', () => {
    expect([nextTheme('dark'), nextTheme('light'), nextTheme('system')]).toEqual(['light', 'system', 'dark'])
  })
})
