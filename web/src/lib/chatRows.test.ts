// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { describe, expect, it } from 'vitest'
import type { AgentItem, ChatLine } from '../api/types'
import { buildRows } from './chatRows'

const L = (line: unknown) => line as ChatLine['line']

const use = (id: number, agent: string, toolId: string): ChatLine => ({
  id,
  agent_id: agent,
  ts: null,
  offset: id,
  line: L({
    type: 'assistant',
    message: { content: [{ type: 'tool_use', id: toolId, name: 'Task', input: { description: 'go' } }] },
  }),
})
const res = (id: number, agent: string, toolId: string): ChatLine => ({
  id,
  agent_id: agent,
  ts: null,
  offset: id,
  line: L({ type: 'user', message: { content: [{ type: 'tool_result', tool_use_id: toolId, content: 'done' }] } }),
})
const say = (id: number, agent: string): ChatLine => ({
  id,
  agent_id: agent,
  ts: null,
  offset: id,
  line: L({ type: 'assistant', message: { content: 'hi' } }),
})
const sub = (merged: string | null) =>
  ({
    agent_id: 'a',
    agent_type: 'Explore',
    description: 'look',
    parent_agent_id: 'main',
    spawn_tool_use_id: 't1',
    merged_at: merged,
  }) as AgentItem
const lanes = new Map([
  ['main', 0],
  ['a', 1],
])
const lines = [use(1, 'main', 't1'), say(2, 'a'), say(3, 'a'), res(4, 'main', 't1')]

describe('buildRows', () => {
  it('emits branch, entries and merge', () => {
    const r = buildRows(lines, [sub('x')], lanes, new Set())
    expect(r.map((x) => x.kind)).toEqual(['branch', 'entry', 'entry', 'merge'])
    expect(r[1]).toMatchObject({ lane: 1 })
    expect(r[0]).toMatchObject({ open: false })
  })
  it('emits a merge row when the result is loaded even without merged_at', () => {
    const r = buildRows(lines, [sub(null)], lanes, new Set())
    expect(r.map((x) => x.kind)).toEqual(['branch', 'entry', 'entry', 'merge'])
    expect(r[0]).toMatchObject({ open: false })
  })
  it('pluralises one step', () => {
    const r = buildRows(
      [lines[0], say(2, 'a'), lines[3]].filter(Boolean) as ChatLine[],
      [sub('x')],
      lanes,
      new Set(['a']),
    )
    expect(r[1]).toMatchObject({ summary: '1 step' })
  })
  it('leaves an unfinished agent without a merge row', () => {
    const r = buildRows(lines.slice(0, 3), [sub(null)], lanes, new Set())
    expect(r.map((x) => x.kind)).toEqual(['branch', 'entry', 'entry'])
    expect(r[0]).toMatchObject({ open: true })
  })
  it('collapses to branch and merge with a step count', () => {
    const r = buildRows(lines, [sub('x')], lanes, new Set(['a']))
    expect(r.map((x) => x.kind)).toEqual(['branch', 'merge'])
    expect(r[0]).toMatchObject({ label: expect.stringContaining('2 steps') })
    expect(r[1]).toMatchObject({ summary: '2 steps' })
  })
  it('merges a background agent at its task notification and keeps the launch ack an entry', () => {
    const note = (id: number, toolId: string): ChatLine => ({
      id,
      agent_id: 'main',
      ts: null,
      offset: id,
      line: L({
        type: 'queue-operation',
        operation: 'enqueue',
        content: `<task-notification><tool-use-id>${toolId}</tool-use-id><duration_ms>9</duration_ms></task-notification>`,
      }),
    })
    const bg = { ...sub(null), background: true } as AgentItem
    const launched = [use(1, 'main', 't1'), res(2, 'main', 't1'), say(3, 'a')]
    const open = buildRows(launched, [bg], lanes, new Set())
    expect(open.map((x) => x.kind)).toEqual(['branch', 'entry', 'entry'])
    expect(open[0]).toMatchObject({ open: true })
    expect(open[1]).toMatchObject({ kind: 'entry', entry: { kind: 'tool_result' } })
    const done = buildRows([...launched, say(4, 'main'), note(5, 't1')], [{ ...bg, merged_at: 'x' }], lanes, new Set())
    expect(done.map((x) => x.kind)).toEqual(['branch', 'entry', 'entry', 'entry', 'merge'])
    expect(done[0]).toMatchObject({ open: false })
    expect(done[4]).toMatchObject({ lane: 1, into: 0, key: 'merge:5:0' })
  })
  it('hides bookkeeping lines and shows an API error as a failure', () => {
    const at = (id: number, line: unknown): ChatLine => ({ id, agent_id: 'main', ts: null, offset: id, line: L(line) })
    const r = buildRows(
      [
        at(1, { type: 'attachment', attachment: { type: 'hook' } }),
        at(2, { type: 'system', subtype: 'turn_duration' }),
        at(3, { type: 'system', subtype: 'stop_hook_summary' }),
        at(4, { type: 'queue-operation', operation: 'dequeue' }),
        at(5, { type: 'bridge-session' }),
        at(6, { type: 'file-history-snapshot' }),
        say(7, 'main'),
        at(8, { type: 'assistant', isApiErrorMessage: true, error: 'rate_limit', message: { content: [] } }),
      ],
      [],
      lanes,
      new Set(),
    )
    expect(r.map((x) => (x.kind === 'entry' ? [x.entry.kind, x.entry.text] : x.kind))).toEqual([
      ['reply', 'hi'],
      ['system', 'API error: rate_limit'],
    ])
  })
})
