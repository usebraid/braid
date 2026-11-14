// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import type { LineItem } from '../api/types'

export type Entry = {
  key: string
  lineId: number
  at: string | null
  kind: 'prompt' | 'reply' | 'thinking' | 'tool_call' | 'tool_result' | 'system' | 'event'
  text: string
  truncated: boolean
  tool?: { id: string; name: string }
  isError?: boolean
}

export const MAX_TEXT = 4000

type Obj = Record<string, unknown>
const isObj = (v: unknown): v is Obj => typeof v === 'object' && v !== null && !Array.isArray(v)
const str = (v: unknown): string => (typeof v === 'string' ? v : '')

function resultText(c: unknown): string {
  if (typeof c === 'string') return c
  if (!Array.isArray(c)) return ''
  return c.map((b) => (isObj(b) && b.type === 'text' ? str(b.text) : '')).join('')
}

function toolText(input: unknown): string {
  if (!isObj(input)) return ''
  const v = input.command ?? input.file_path ?? input.pattern ?? input.description
  return typeof v === 'string' ? v : (JSON.stringify(input) ?? '').slice(0, 200)
}

// One transcript line becomes 0..n entries. Unknown shapes never throw and never leak their content.
export function entriesOf(item: LineItem): Entry[] {
  const out: Entry[] = []
  const l: unknown = item.line
  const add = (kind: Entry['kind'], text: string, extra: Partial<Entry> = {}) => {
    const truncated = text.length > MAX_TEXT
    out.push({
      key: `${item.id}:${out.length}`,
      lineId: item.id,
      at: isObj(l) && typeof l.timestamp === 'string' ? l.timestamp : null,
      kind,
      text: truncated ? text.slice(0, MAX_TEXT) : text,
      truncated,
      ...extra,
    })
  }
  if (!isObj(l)) return out
  if ('invalid_json_b64' in l) {
    add('event', 'unparsable line')
    return out
  }
  const content = isObj(l.message) ? l.message.content : undefined
  const blocks = Array.isArray(content) ? content.filter(isObj) : []
  switch (l.type) {
    case 'user':
      if (typeof content === 'string') add('prompt', content)
      for (const b of blocks) {
        if (b.type === 'text') add('prompt', str(b.text))
        else if (b.type === 'tool_result')
          add('tool_result', resultText(b.content), {
            tool: { id: str(b.tool_use_id), name: '' },
            isError: b.is_error === true,
          })
      }
      break
    case 'assistant':
      if (typeof content === 'string') add('reply', content)
      for (const b of blocks) {
        if (b.type === 'text') add('reply', str(b.text))
        else if (b.type === 'thinking') add('thinking', str(b.thinking))
        else if (b.type === 'tool_use')
          add('tool_call', toolText(b.input), { tool: { id: str(b.id), name: str(b.name) } })
      }
      break
    case 'system':
      add('system', str(l.subtype))
      break
    case 'queue-operation': {
      // a background agent reports back here; the id ties it to the spawning tool call
      const id = /<tool-use-id>([^<]*)<\/tool-use-id>/.exec(str(l.content))?.[1]
      if (l.operation === 'enqueue' && id && str(l.content).includes('<task-notification>'))
        add('event', 'task notification', { tool: { id, name: '' } })
      else add('event', 'queue-operation')
      break
    }
    default:
      add('event', str(l.type).slice(0, 60))
  }
  return out
}
