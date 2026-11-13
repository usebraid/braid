// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// web/src/lib/transcript.test.ts
import { describe, expect, it } from 'vitest'
import type { LineItem } from '../api/types'
import { entriesOf, MAX_TEXT } from './transcript'

const li = (id: number, line: unknown) => ({ id, offset: id * 10, line }) as LineItem

describe('entriesOf', () => {
  it('turns a prompt string and a text block into prompts', () => {
    expect(
      entriesOf(
        li(1, { type: 'user', timestamp: '2026-11-01T10:00:10Z', message: { role: 'user', content: 'hello' } }),
      ),
    ).toMatchObject([{ kind: 'prompt', text: 'hello', lineId: 1, at: '2026-11-01T10:00:10Z' }])
    expect(
      entriesOf(li(2, { type: 'user', message: { role: 'user', content: [{ type: 'text', text: 'hi' }] } })),
    ).toMatchObject([{ kind: 'prompt', text: 'hi' }])
  })
  it('splits an assistant line into reply, thinking and tool calls', () => {
    const e = entriesOf(
      li(3, {
        type: 'assistant',
        message: {
          role: 'assistant',
          content: [
            { type: 'thinking', thinking: 'hmm' },
            { type: 'text', text: 'answer' },
            { type: 'tool_use', id: 't1', name: 'Bash', input: { command: 'go test' } },
          ],
        },
      }),
    )
    expect(e.map((x) => x.kind)).toEqual(['thinking', 'reply', 'tool_call'])
    expect(e[2]).toMatchObject({ text: 'go test', tool: { id: 't1', name: 'Bash' } })
    expect(new Set(e.map((x) => x.key)).size).toBe(3)
  })
  it('reads tool results as string or blocks and keeps the error flag', () => {
    const a = entriesOf(
      li(4, {
        type: 'user',
        message: {
          role: 'user',
          content: [{ type: 'tool_result', tool_use_id: 't1', is_error: true, content: 'exit 1' }],
        },
      }),
    )
    const b = entriesOf(
      li(5, {
        type: 'user',
        message: {
          role: 'user',
          content: [{ type: 'tool_result', tool_use_id: 't1', content: [{ type: 'text', text: 'ok' }] }],
        },
      }),
    )
    expect(a).toMatchObject([{ kind: 'tool_result', text: 'exit 1', isError: true }])
    expect(b).toMatchObject([{ kind: 'tool_result', text: 'ok' }])
  })
  it('never exposes content of unknown line types and survives junk', () => {
    expect(entriesOf(li(6, { type: 'brand-new-type', secret: 'S3CR3T' }))).toMatchObject([
      { kind: 'event', text: 'brand-new-type' },
    ])
    expect(JSON.stringify(entriesOf(li(6, { type: 'brand-new-type', secret: 'S3CR3T' })))).not.toContain('S3CR3T')
    expect(entriesOf(li(7, { invalid_json_b64: 'AAAA' }))).toMatchObject([{ kind: 'event', text: 'unparsable line' }])
    expect(() => entriesOf(li(8, null))).not.toThrow()
    expect(() => entriesOf(li(9, 42))).not.toThrow()
  })
  it('truncates long text', () => {
    const [e] = entriesOf(li(10, { type: 'user', message: { role: 'user', content: 'x'.repeat(MAX_TEXT + 50) } }))
    expect(e?.text).toHaveLength(MAX_TEXT)
    expect(e?.truncated).toBe(true)
  })
})
