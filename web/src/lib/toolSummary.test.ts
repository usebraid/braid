// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { expect, it } from 'vitest'
import { toolSummary } from './toolSummary'

it.each([
  ['Skill', { skill: 'superpowers:brainstorming' }, 'superpowers:brainstorming'],
  ['Task', { subagent_type: 'Explore', description: 'find it', prompt: 'x' }, 'Explore: find it'],
  ['Agent', { description: 'only' }, 'only'],
  ['Read', { file_path: '/a/b.go' }, '/a/b.go'],
  ['Bash', { command: 'ls -la\necho hi' }, 'ls -la'],
  ['Grep', { pattern: 'foo.*', path: '.' }, 'foo.*'],
  ['WebFetch', { url: 'https://x.dev', prompt: 'p' }, 'https://x.dev'],
  ['TodoWrite', { todos: [{}, {}, {}] }, '3 todos'],
  ['Custom', { n: 1, q: 'hello', z: 'w' }, 'q: hello'],
  ['Custom', { n: 1 }, ''],
  ['Read', null, ''],
])('%s %j', (name, input, want) => {
  expect(toolSummary(name, input)).toBe(want)
})
