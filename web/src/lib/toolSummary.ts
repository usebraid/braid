// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

const MAX = 200
type Obj = Record<string, unknown>
const s = (v: unknown) => (typeof v === 'string' ? v : '')
const first = (v: unknown) => s(v).split('\n')[0] ?? ''

// A readable one-liner for the graph; the List view keeps its own text.
export function toolSummary(name: string, input: unknown): string {
  if (typeof input !== 'object' || input === null || Array.isArray(input)) return ''
  const i = input as Obj
  let out: string
  switch (name) {
    case 'Skill':
      out = s(i.skill ?? i.name)
      break
    case 'Task':
    case 'Agent': {
      const type = s(i.subagent_type)
      out = [type, s(i.description)].filter(Boolean).join(': ')
      break
    }
    case 'Read':
    case 'Edit':
    case 'Write':
    case 'MultiEdit':
      out = s(i.file_path ?? i.path)
      break
    case 'Bash':
      out = first(i.command)
      break
    case 'Grep':
    case 'Glob':
      out = s(i.pattern)
      break
    case 'WebFetch':
      out = s(i.url)
      break
    case 'TodoWrite':
      out = Array.isArray(i.todos) ? `${i.todos.length} todos` : ''
      break
    default:
      out = ''
  }
  if (!out) {
    // never a JSON blob: the first string field as key: value
    const kv = Object.entries(i).find(([, v]) => typeof v === 'string' && v !== '')
    out = kv ? `${kv[0]}: ${first(kv[1])}` : ''
  }
  return out.length > MAX ? `${out.slice(0, MAX)}…` : out
}
