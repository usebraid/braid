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
  describe('background agent whose launch ack is only a tool result and whose hand-back is a user-role line', () => {
    // regression: the API reports the launch ack as merged_at (background=false); the hand-back is a user-role peer line
    const at = (id: number, agent: string, line: unknown, ts: string): ChatLine => ({
      id,
      agent_id: agent,
      ts,
      offset: id,
      line: L(line),
    })
    const handBack = (id: number) =>
      at(
        id,
        'main',
        {
          type: 'user',
          message: { content: 'framed' },
          origin: { kind: 'peer', senderTaskId: 'a', handback: true, handbackReportText: 'report' },
        },
        '2026-11-01T00:00:20Z',
      )
    const queued = (id: number) =>
      at(
        id,
        'main',
        {
          type: 'queue-operation',
          operation: 'enqueue',
          content: '<task-notification><tool-use-id>t1</tool-use-id><summary>done</summary></task-notification>',
        },
        '2026-11-01T00:00:21Z',
      )
    const agent = { ...sub('2026-11-01T00:00:20Z'), background: true } as AgentItem
    const ls = [use(1, 'main', 't1'), res(2, 'main', 't1'), say(3, 'a'), say(4, 'a'), handBack(5), queued(6)]
    it('merges once, at the hand-back, and shows no prompt bubble', () => {
      const r = buildRows(ls, [agent], lanes, new Set())
      expect(r.map((x) => x.kind)).toEqual(['branch', 'entry', 'entry', 'entry', 'merge'])
      expect(r[1]).toMatchObject({ entry: { kind: 'tool_result' } })
      expect(r[4]).toMatchObject({ lane: 1, into: 0, summary: 'report' })
      expect(r[0]).toMatchObject({ open: false })
      expect(r.some((x) => x.kind === 'entry' && x.entry.kind === 'prompt')).toBe(false)
    })
    it('puts the merge row after the last row of the agent when its lines are stamped after the hand-back', () => {
      // regression: those last rows came after the merge, outside the lane line
      const r = buildRows([...ls.slice(0, 3), handBack(5), say(6, 'a'), say(7, 'a')], [agent], lanes, new Set())
      expect(r.map((x) => `${x.kind}:${x.lane}`)).toEqual([
        'branch:1',
        'entry:0',
        'entry:1',
        'entry:1',
        'entry:1',
        'merge:1',
      ])
    })
    it('runs a resumed agent to its last hand-back, with arrows for the exchange in between', () => {
      // regression: an agent continued by SendMessage merged at its first hand-back; its later rows had no lane
      const msg = (id: number, to: string) =>
        at(
          id,
          'main',
          {
            type: 'assistant',
            message: { content: [{ type: 'tool_use', id: `m${id}`, name: 'SendMessage', input: { to } }] },
          },
          '2026-11-01T00:00:30Z',
        )
      const r = buildRows([...ls, msg(7, 'a'), say(8, 'a'), handBack(9), queued(10)], [agent], lanes, new Set())
      expect(
        r.map((x) => `${x.kind}:${x.lane}${x.kind === 'entry' && x.arrow ? `:${x.arrow.dir}${x.arrow.lane}` : ''}`),
      ).toEqual([
        'branch:1',
        'entry:0', // launch ack
        'entry:1',
        'entry:1',
        'entry:0:in1', // first hand-back (its queue line is a repeat): an arrow back, the lane runs on
        'entry:0:out1', // SendMessage to the agent
        'entry:1',
        'merge:1', // the last hand-back ends the lane
      ])
    })
    it('counts a queue line stamped before the last lines of the agent as the same hand-back', () => {
      // regression (review): any agent line after a hand-back counted as a resume, so the queue line became a false arrow
      const r = buildRows([...ls.slice(0, 3), queued(6), say(7, 'a'), handBack(8)], [agent], lanes, new Set())
      expect(r.some((x) => x.kind === 'entry' && x.arrow)).toBe(false)
      expect(r.filter((x) => x.kind === 'merge')).toHaveLength(1)
    })
    it('counts a second hand-back after a SendMessage even when the agent wrote nothing in between', () => {
      const msg = at(
        7,
        'main',
        {
          type: 'assistant',
          message: { content: [{ type: 'tool_use', id: 'm', name: 'SendMessage', input: { to: 'a' } }] },
        },
        '2026-11-01T00:00:30Z',
      )
      const r = buildRows([...ls.slice(0, 3), handBack(5), msg, handBack(9)], [agent], lanes, new Set())
      expect(r.map((x) => (x.kind === 'entry' && x.arrow ? x.arrow.dir : x.kind)).slice(-3)).toEqual([
        'in',
        'out',
        'merge',
      ])
    })
    it('draws no arrows into a collapsed agent and no hand-back rows inside a collapsed parent', () => {
      const msg = at(
        7,
        'main',
        {
          type: 'assistant',
          message: { content: [{ type: 'tool_use', id: 'm', name: 'SendMessage', input: { to: 'a' } }] },
        },
        '2026-11-01T00:00:30Z',
      )
      const r = buildRows([...ls, msg, handBack(9)], [agent], lanes, new Set(['a']))
      expect(r.some((x) => x.kind === 'entry' && x.arrow)).toBe(false)
      const nested = { ...agent, agent_id: 'b', parent_agent_id: 'p', spawn_tool_use_id: 'tb' } as AgentItem
      const parent = { ...sub('x'), agent_id: 'p', spawn_tool_use_id: 'tp' } as AgentItem
      const peer = (id: number) =>
        at(
          id,
          'main',
          {
            type: 'user',
            message: { content: 'framed' },
            origin: { kind: 'peer', senderTaskId: 'b', handback: true, handbackReportText: 'r' },
          },
          '2026-11-01T00:00:40Z',
        )
      const bmsg = at(
        11,
        'main',
        {
          type: 'assistant',
          message: { content: [{ type: 'tool_use', id: 'm2', name: 'SendMessage', input: { to: 'b' } }] },
        },
        '2026-11-01T00:00:41Z',
      )
      const rr = buildRows(
        [peer(10), bmsg, peer(12)],
        [parent, nested],
        new Map([
          ['main', 0],
          ['p', 1],
          ['b', 2],
        ]),
        new Set(['p']),
      )
      expect(rr.some((x) => x.kind === 'entry' && x.agentId === 'p')).toBe(false)
    })
    it('draws no arrow for a SendMessage to an unknown or ambiguous agent', () => {
      const msg = (to: string) =>
        at(
          7,
          'main',
          {
            type: 'assistant',
            message: { content: [{ type: 'tool_use', id: 'm', name: 'SendMessage', input: { to } }] },
          },
          '2026-11-01T00:00:30Z',
        )
      for (const to of ['zzzzzzzz', 'b', ''])
        expect(buildRows([msg(to)], [agent], lanes, new Set())[0]).not.toHaveProperty('arrow')
    })
    it('stays open until the hand-back is there', () => {
      const r = buildRows(ls.slice(0, 4), [{ ...agent, merged_at: null }], lanes, new Set())
      expect(r.map((x) => x.kind)).toEqual(['branch', 'entry', 'entry', 'entry'])
      expect(r[0]).toMatchObject({ open: true })
    })
  })
  describe('dot types', () => {
    const one = (id: number, line: unknown): ChatLine => ({ id, agent_id: 'main', ts: null, offset: id, line: L(line) })
    const asst = (id: number, name: string, input: unknown, tid = `x${id}`) =>
      one(id, { type: 'assistant', message: { content: [{ type: 'tool_use', id: tid, name, input }] } })
    const dots = (ls: ChatLine[]) =>
      buildRows(ls, [], lanes, new Set()).map((r) => (r.kind === 'entry' ? r.dot : r.kind))
    it('tells failed calls, skills, background commands and their notification, compaction and interrupts apart', () => {
      const note =
        '<task-notification><tool-use-id>bg1</tool-use-id><summary>build finished</summary></task-notification>'
      const r = buildRows(
        [
          asst(1, 'Bash', { command: 'go test' }),
          one(2, {
            type: 'user',
            message: { content: [{ type: 'tool_result', tool_use_id: 'x1', content: 'no', is_error: true }] },
          }),
          asst(3, 'Skill', { skill: 'superpowers:brainstorming' }),
          asst(4, 'Bash', { command: 'sleep 9', run_in_background: true }, 'bg1'),
          one(5, { type: 'system', subtype: 'compact_boundary' }),
          one(6, { type: 'user', message: { content: [{ type: 'text', text: '[Request interrupted by user]' }] } }),
          one(7, { type: 'queue-operation', operation: 'enqueue', content: note }),
          one(8, { type: 'queue-operation', operation: 'enqueue', content: note.replace('bg1', 'other') }),
          say(9, 'main'),
        ],
        [],
        lanes,
        new Set(),
      )
      expect(r.map((x) => (x.kind === 'entry' ? x.dot : x.kind))).toEqual([
        'ring',
        'fail',
        'skill',
        'bgcmd',
        'compaction',
        'interrupt',
        'bgcmd', // the notification of the background command shares its dot
        'notice',
        'reply',
      ])
      expect(r[2]).toMatchObject({ skill: 'superpowers:brainstorming' })
      expect(dots([asst(1, 'Bash', { command: 'x', run_in_background: false })])).toEqual(['ring'])
    })
    it('marks the branch and merge rows of a background agent (merge reads "reported back")', () => {
      const bg = { ...sub('x'), background: true } as AgentItem
      const r = buildRows(
        [
          ...lines.slice(0, 3),
          res(4, 'main', 't1'),
          one(5, {
            type: 'queue-operation',
            operation: 'enqueue',
            content: '<task-notification><tool-use-id>t1</tool-use-id><summary>s</summary></task-notification>',
          }),
        ],
        [bg],
        lanes,
        new Set(),
      )
      expect(r.filter((x) => x.kind === 'branch' || x.kind === 'merge')).toMatchObject([
        { kind: 'branch', background: true },
        { kind: 'merge', background: true, summary: 's' },
      ])
      expect(buildRows(lines, [{ ...sub('x'), background: false } as AgentItem], lanes, new Set())[0]).toMatchObject({
        background: false,
      })
    })
  })
  describe('review fixes', () => {
    const at = (id: number, agent: string, line: unknown): ChatLine => ({
      id,
      agent_id: agent,
      ts: null,
      offset: id,
      line: L(line),
    })
    const queue = (id: number, tool: string) =>
      at(id, 'main', {
        type: 'queue-operation',
        operation: 'enqueue',
        content: `<task-notification><tool-use-id>${tool}</tool-use-id><summary>s</summary></task-notification>`,
      })
    it('merges a background agent spawned by a subagent at its notification in the main file', () => {
      const a = { ...sub('x'), agent_id: 'a', spawn_tool_use_id: 't1' } as AgentItem
      const b = {
        ...sub('x'),
        agent_id: 'b',
        parent_agent_id: 'a',
        spawn_tool_use_id: 't2',
        background: true,
      } as AgentItem
      const lanes3 = new Map([
        ['main', 0],
        ['a', 1],
        ['b', 2],
      ])
      const r = buildRows(
        [use(1, 'main', 't1'), say(2, 'a'), use(3, 'a', 't2'), say(4, 'b'), queue(5, 't2'), say(6, 'a')],
        [a, b],
        lanes3,
        new Set(),
      )
      const m = r.find((x) => x.kind === 'merge' && x.agentId === 'b')
      expect(m).toMatchObject({ lane: 2, into: 1 })
    })
    it('shows the notification of a background command once, whichever line carries it', () => {
      const userNote = at(6, 'main', {
        type: 'user',
        origin: { kind: 'task-notification' },
        message: { content: '<task-notification><tool-use-id>bg</tool-use-id></task-notification>' },
      })
      const r = buildRows(
        [
          at(1, 'main', {
            type: 'assistant',
            message: {
              content: [{ type: 'tool_use', id: 'bg', name: 'Bash', input: { command: 'x', run_in_background: true } }],
            },
          }),
          queue(5, 'bg'),
          userNote,
        ],
        [],
        lanes,
        new Set(),
      )
      expect(r.filter((x) => x.kind === 'entry' && x.dot === 'bgcmd')).toHaveLength(2)
    })
  })
})
