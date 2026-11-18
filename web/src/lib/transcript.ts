// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import type { LineItem } from '../api/types'

export type Entry = {
  key: string
  lineId: number
  at: string | null
  kind:
    | 'prompt'
    | 'reply'
    | 'thinking'
    | 'tool_call'
    | 'tool_result'
    | 'system'
    | 'event'
    | 'notification'
    | 'compaction'
    | 'interrupt'
  text: string
  truncated: boolean
  tool?: { id: string; name: string; input?: unknown }
  background?: boolean // a Bash or PowerShell call run with run_in_background
  from?: string // a notification's sender: the agent id of a subagent hand-back
  isError?: boolean
}

export const MAX_TEXT = 4000
const INTERRUPT = /^\[Request interrupted by user/
const SHELLS = new Set(['Bash', 'PowerShell'])

type Obj = Record<string, unknown>
const isObj = (v: unknown): v is Obj => typeof v === 'object' && v !== null && !Array.isArray(v)
const str = (v: unknown): string => (typeof v === 'string' ? v : '')

function resultText(c: unknown): string {
  if (typeof c === 'string') return c
  if (!Array.isArray(c)) return ''
  return c.map((b) => (isObj(b) && b.type === 'text' ? str(b.text) : '')).join('')
}

// Lazy, so a value may hold `<`; the input is capped first so unclosed tags cannot make the scan quadratic.
const TAG_SCAN = 64 << 10
const tagOf = (s: string, tag: string) =>
  new RegExp(`<${tag}>([\\s\\S]*?)</${tag}>`).exec(s.slice(0, TAG_SCAN))?.[1]?.trim() ?? ''
// Only a text that is one task-notification element counts; a prompt that merely quotes the tag stays a prompt.
const NOTIFICATION = /^\s*<task-notification>[\s\S]*<\/task-notification>\s*$/
// What a notification says, never its tag names.
const noteText = (s: string) => tagOf(s, 'summary') || tagOf(s, 'status')

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
    case 'user': {
      // Claude Code injects task notifications and subagent hand-backs as user-role lines; they are not the user speaking.
      const o = isObj(l.origin) ? l.origin : {}
      const asText =
        typeof content === 'string' ? content : blocks.map((b) => (b.type === 'text' ? str(b.text) : '')).join('')
      if ((typeof content === 'string' || blocks.length > 0) && o.kind === 'peer' && o.handback === true) {
        const body = typeof o.handbackReportText === 'string' ? o.handbackReportText : asText
        add('notification', body, { from: str(o.senderTaskId) })
        break
      }
      if (l.isCompactSummary === true) {
        add('event', 'compact summary') // the summary the harness feeds back after a compaction is not the user speaking
        break
      }
      if (typeof content === 'string' && (o.kind === 'task-notification' || NOTIFICATION.test(content))) {
        add('notification', noteText(content), {
          tool: { id: tagOf(content, 'tool-use-id'), name: '' },
        })
        break
      }
      if (typeof content === 'string') add(INTERRUPT.test(content) ? 'interrupt' : 'prompt', content)
      for (const b of blocks) {
        if (b.type === 'text') add(INTERRUPT.test(str(b.text)) ? 'interrupt' : 'prompt', str(b.text))
        else if (b.type === 'tool_result')
          add('tool_result', resultText(b.content), {
            tool: { id: str(b.tool_use_id), name: '' },
            isError: b.is_error === true,
          })
      }
      break
    }
    case 'assistant':
      if (typeof content === 'string') add('reply', content)
      for (const b of blocks) {
        if (b.type === 'text') add('reply', str(b.text))
        else if (b.type === 'thinking') add('thinking', str(b.thinking))
        else if (b.type === 'tool_use')
          add('tool_call', toolText(b.input), {
            tool: { id: str(b.id), name: str(b.name), input: b.input },
            ...(isObj(b.input) && b.input.run_in_background === true && SHELLS.has(str(b.name))
              ? { background: true }
              : {}),
          })
      }
      break
    case 'system':
      if (l.subtype === 'compact_boundary') add('compaction', 'Conversation compacted')
      else add('system', str(l.subtype))
      break
    case 'queue-operation': {
      // a background agent reports back here; the id ties it to the spawning tool call
      const id = tagOf(str(l.content), 'tool-use-id')
      if (l.operation === 'enqueue' && id && NOTIFICATION.test(str(l.content)))
        add('notification', noteText(str(l.content)), { tool: { id, name: '' } })
      else add('event', 'queue-operation')
      break
    }
    default:
      add('event', str(l.type).slice(0, 60))
  }
  return out
}
