// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors
import { defineConfig } from 'vitest/config'

export default defineConfig({
  // Claude Code supplies 'claude-code' at runtime; CI uses a small fake.
  resolve: { alias: { 'claude-code': new URL('./tests/claude-code.fake.ts', import.meta.url).pathname } },
  oxc: { jsx: { runtime: 'classic', pragma: 'h', pragmaFrag: 'Fragment', development: false } },
})
