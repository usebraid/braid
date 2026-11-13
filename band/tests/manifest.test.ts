// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors
import { existsSync, readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, test } from 'vitest'

const band = join(import.meta.dirname, '..')
const json = (...p: string[]) => JSON.parse(readFileSync(join(band, ...p), 'utf8'))

describe('zero token cost', () => {
  test('claude-band.json has empty hooks and exactly one module', () => {
    const c = json('hooks', 'claude-band.json')
    expect(c.hooks).toEqual({})
    expect(c.modules).toEqual(['./band/register.tsx'])
    expect(existsSync(join(band, 'hooks', 'band', 'register.tsx'))).toBe(true)
  })
  test('the plugin declares no model-facing surface', () => {
    const p = json('.claude-plugin', 'plugin.json')
    expect(p.name).toBe('braid')
    expect(p.hooks).toEqual(['./hooks/claude-band.json'])
    for (const k of ['skills', 'commands', 'agents', 'mcpServers', 'userConfig', 'lspServers'])
      expect(p).not.toHaveProperty(k)
  })
  test('the plugin directory ships no skills, commands, agents or extra hook files', () => {
    for (const d of ['skills', 'commands', 'agents']) expect(existsSync(join(band, d))).toBe(false)
    expect(readdirSync(join(band, 'hooks')).filter((f) => f.endsWith('.json'))).toEqual(['claude-band.json'])
    expect(existsSync(join(band, '.mcp.json'))).toBe(false)
  })
  test('the marketplace points at ./band', () => {
    const m = json('..', '.claude-plugin', 'marketplace.json')
    expect(m.plugins).toEqual([expect.objectContaining({ name: 'braid', source: './band' })])
  })
})
