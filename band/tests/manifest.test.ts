// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors
import { existsSync, readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, test } from 'vitest'

const band = join(import.meta.dirname, '..')
const json = (...p: string[]) => JSON.parse(readFileSync(join(band, ...p), 'utf8'))

const plugin = join(band, 'plugin')
const pjson = (...p: string[]) => JSON.parse(readFileSync(join(plugin, ...p), 'utf8'))

describe('zero token cost', () => {
  test('hooks.json holds exactly one module', () => {
    const c = pjson('hooks', 'hooks.json')
    expect(c).toEqual({ modules: ['./register.tsx'] })
    expect(existsSync(join(plugin, 'hooks', 'register.tsx'))).toBe(true)
  })
  test('the plugin declares no model-facing surface', () => {
    const p = pjson('.claude-plugin', 'plugin.json')
    expect(p.name).toBe('braid')
    expect(p.types).toBe('./types/index.d.ts')
    for (const k of ['hooks', 'skills', 'commands', 'agents', 'mcpServers', 'userConfig', 'lspServers'])
      expect(p).not.toHaveProperty(k)
  })
  test('the plugin directory ships no dev tooling, skills, commands, agents or extra hook files', () => {
    for (const d of ['skills', 'commands', 'agents', 'node_modules', 'tests'])
      expect(existsSync(join(plugin, d))).toBe(false)
    for (const f of ['package.json', 'package-lock.json', '.mcp.json']) expect(existsSync(join(plugin, f))).toBe(false)
    expect(readdirSync(join(plugin, 'hooks')).filter((f) => f.endsWith('.json'))).toEqual(['hooks.json'])
  })
  test('the marketplace points at ./band/plugin', () => {
    const m = json('..', '.claude-plugin', 'marketplace.json')
    expect(m.plugins).toEqual([expect.objectContaining({ name: 'braid', source: './band/plugin' })])
  })
})
