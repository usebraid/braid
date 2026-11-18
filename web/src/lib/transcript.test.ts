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
  it('reads a subagent hand-back and a task notification as notifications, not prompts', () => {
    const peer = {
      type: 'user',
      message: { role: 'user', content: 'framed report' },
      origin: { kind: 'peer', senderTaskId: 'a9', handback: true, handbackReportText: 'the report' },
    }
    expect(entriesOf(li(11, peer))).toMatchObject([{ kind: 'notification', text: 'the report', from: 'a9' }])
    const note = '<task-notification><tool-use-id>t7</tool-use-id><summary>did it</summary></task-notification>'
    expect(entriesOf(li(12, { type: 'user', message: { role: 'user', content: note } }))).toMatchObject([
      { kind: 'notification', text: 'did it', tool: { id: 't7' } },
    ])
    expect(entriesOf(li(13, { type: 'queue-operation', operation: 'enqueue', content: note }))).toMatchObject([
      { kind: 'notification', text: 'did it', tool: { id: 't7' } },
    ])
    expect(
      entriesOf(li(14, { type: 'user', message: { role: 'user', content: 'plain' }, origin: { kind: 'human' } })),
    ).toMatchObject([{ kind: 'prompt' }])
  })
  it('marks compaction, interrupts and background shell calls, and hides the compact summary', () => {
    expect(entriesOf(li(20, { type: 'system', subtype: 'compact_boundary' }))).toMatchObject([{ kind: 'compaction' }])
    const user = (content: unknown, extra = {}) =>
      li(21, { type: 'user', message: { role: 'user', content }, ...extra })
    expect(entriesOf(user([{ type: 'text', text: '[Request interrupted by user for tool use]' }]))).toMatchObject([
      { kind: 'interrupt' },
    ])
    expect(entriesOf(user('summary of before', { isCompactSummary: true }))).toMatchObject([{ kind: 'event' }])
    const call = (name: string, input: unknown) =>
      entriesOf(li(22, { type: 'assistant', message: { content: [{ type: 'tool_use', id: 'x', name, input }] } }))[0]
    expect(call('Bash', { command: 'sleep 9', run_in_background: true })?.background).toBe(true)
    expect(call('PowerShell', { command: 'sleep 9', run_in_background: true })?.background).toBe(true)
    expect(call('Bash', { command: 'ls' })?.background).toBeUndefined()
    expect(call('Read', { run_in_background: true })?.background).toBeUndefined()
  })
  describe('notification hardening', () => {
    const user = (content: unknown, extra = {}) =>
      li(30, { type: 'user', message: { role: 'user', content }, ...extra })
    const note = (inner: string) => `<task-notification>${inner}</task-notification>`
    it('keeps a prompt that merely quotes the tag a prompt', () => {
      expect(entriesOf(user('why does <task-notification> show up?'))[0]).toMatchObject({ kind: 'prompt' })
      expect(entriesOf(user(`look: ${note('<tool-use-id>t1</tool-use-id>')} and more`))[0]).toMatchObject({
        kind: 'prompt',
      })
      expect(
        entriesOf(
          li(31, {
            type: 'queue-operation',
            operation: 'enqueue',
            content: `x ${note('<tool-use-id>t1</tool-use-id>')}`,
          }),
        )[0],
      ).toMatchObject({ kind: 'event' })
      expect(
        entriesOf(user(note('<summary>ok</summary>'), { origin: { kind: 'task-notification' } }))[0],
      ).toMatchObject({ kind: 'notification' })
    })
    it('reads a summary that contains < and never shows tag names as text', () => {
      expect(entriesOf(user(note('<tool-use-id>t</tool-use-id><summary>Fix Array<T> typing</summary>')))[0]?.text).toBe(
        'Fix Array<T> typing',
      )
      expect(entriesOf(user(note('<tool-use-id>t</tool-use-id>')))[0]).toMatchObject({ kind: 'notification', text: '' })
      expect(entriesOf(user(note('<tool-use-id>t</tool-use-id><status>completed</status>')))[0]?.text).toBe('completed')
    })
    it('treats a hand-back with array content as a notification', () => {
      const e = entriesOf(
        user([{ type: 'text', text: 'the report' }], { origin: { kind: 'peer', handback: true, senderTaskId: 'a1' } }),
      )
      expect(e).toMatchObject([{ kind: 'notification', text: 'the report', from: 'a1' }])
    })
    it('scans a huge unclosed tag run in bounded time', () => {
      const t0 = performance.now()
      entriesOf(user(note('<summary>'.repeat(400_000))))
      expect(performance.now() - t0).toBeLessThan(500)
    })
  })
})
