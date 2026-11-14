// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// web/src/lib/graph.test.ts
import { describe, expect, it } from 'vitest'
import type { AgentItem, SessionDetail } from '../api/types'
import { type AgentNodeData, buildGraph, MAX_CHILDREN, NODE_HEIGHT, NODE_WIDTH, skillColor } from './graph'

const usage = { cost_usd: 0, tokens: 0, input: 0, output: 0, cache_read: 0, cache_write: 0, messages: 0 }
const agent = (id: string, parent: string | null, over: Partial<AgentItem> = {}): AgentItem => ({
  agent_id: id,
  agent_type: id === 'main' ? 'main' : 'Explore',
  description: `d-${id}`,
  parent_agent_id: parent,
  spawn_tool_use_id: null,
  spawned_at: null,
  merged_at: null,
  background: false,
  spawn_depth: 0,
  started_at: null,
  ended_at: null,
  finished: null,
  duration_ms: 1000,
  duration_estimated: false,
  tool_calls: {},
  tool_errors: 0,
  usage,
  ...over,
})
const detail = {
  session: {} as SessionDetail['session'],
  agents: [agent('main', null), agent('a1', 'main'), agent('a2', 'main'), agent('a3', 'a1')],
  skill_phases: [
    { agent_id: 'main', seq: 1, skill: 'superpowers:brainstorming', started_at: null, ended_at: null },
    { agent_id: 'main', seq: 2, skill: 'superpowers:writing-plans', started_at: null, ended_at: null },
  ],
  failures: [
    {
      category: 'rate_limit',
      detail: '',
      occurred_at: '2026-11-01T10:05:00Z',
      session_id: 'g1',
      agent_id: 'a2',
      line_id: 5,
      limit_type: null,
      resets_at: null,
      time_lost_s: null,
    },
  ],
} as SessionDetail

describe('buildGraph', () => {
  const g = buildGraph(detail)
  const node = (id: string) => {
    const n = g.nodes.find((x) => x.id === id)
    if (!n) throw new Error(id)
    return n
  }
  const data = (id: string) => node(id).data as AgentNodeData
  it('has one node per agent and unlabelled spawn edges (the node carries the description)', () => {
    expect(g.nodes.map((n) => n.id)).toEqual(['a1', 'a2', 'a3', 'main'])
    expect(g.edges.map((e) => [e.source, e.target, e.label]).sort()).toEqual([
      ['a1', 'a3', undefined],
      ['main', 'a1', undefined],
      ['main', 'a2', undefined],
    ])
  })
  it('lays out left to right: parents left of children, siblings stacked in one column, no overlap', () => {
    expect(node('main').position.x).toBeLessThan(node('a1').position.x)
    expect(node('a1').position.x).toBeLessThan(node('a3').position.x)
    expect(node('a1').position.x).toBe(node('a2').position.x)
    expect(Math.abs(node('a1').position.y - node('a2').position.y)).toBeGreaterThanOrEqual(NODE_HEIGHT)
    expect(node('a1').position.x - node('main').position.x).toBeGreaterThanOrEqual(NODE_WIDTH)
  })
  it('keeps every node the same size and carries phases and failures in data', () => {
    expect(new Set(g.nodes.map((n) => `${n.width}x${n.height}`))).toEqual(new Set([`${NODE_WIDTH}x${NODE_HEIGHT}`]))
    expect(data('main').phases.map((p) => p.skill)).toEqual(['superpowers:brainstorming', 'superpowers:writing-plans'])
    expect(data('a2').failures).toHaveLength(1)
  })
  it('is deterministic', () => {
    expect(JSON.stringify(buildGraph(detail))).toBe(JSON.stringify(g))
  })
  it('attaches an orphan agent to main with a dashed unlabeled edge', () => {
    const o = buildGraph({ ...detail, agents: [agent('main', null), agent('x', null)] })
    expect(o.edges).toEqual([expect.objectContaining({ source: 'main', target: 'x', data: { orphan: true } })])
  })
  it('groups more than MAX_CHILDREN children into one "+N more" node until the parent is expanded', () => {
    const kids = Array.from({ length: 20 }, (_, i) => agent(`k${String(i).padStart(2, '0')}`, 'main'))
    const grandkid = agent('g1', 'k19')
    const big = { ...detail, agents: [agent('main', null), ...kids, grandkid], skill_phases: [], failures: [] }
    const c = buildGraph(big)
    const more = c.nodes.find((n) => n.id === 'more:main')
    expect(more).toMatchObject({ type: 'more', data: { parent: 'main', hidden: 20 - (MAX_CHILDREN - 1) } })
    expect(c.nodes.filter((n) => n.type === 'agent')).toHaveLength(MAX_CHILDREN) // main + 11 children
    expect(c.nodes.some((n) => n.id === 'g1')).toBe(false) // the hidden child's subtree stays hidden too
    expect(c.edges).toContainEqual(expect.objectContaining({ source: 'main', target: 'more:main' }))
    const open = buildGraph(big, { expanded: new Set(['main']) })
    expect(open.nodes.some((n) => n.type === 'more')).toBe(false)
    expect(open.nodes).toHaveLength(22)
    const twelve = buildGraph({ ...big, agents: [agent('main', null), ...kids.slice(0, MAX_CHILDREN)] })
    expect(twelve.nodes.some((n) => n.type === 'more')).toBe(false)
  })
  it('keeps agents caught in a parent cycle', () => {
    const c = buildGraph({ ...detail, agents: [agent('main', null), agent('x', 'y'), agent('y', 'x')] })
    expect(c.nodes.map((n) => n.id)).toEqual(['main', 'x', 'y'])
  })
  it('colours skills stably', () => {
    expect(skillColor('superpowers:brainstorming')).toBe(skillColor('superpowers:brainstorming'))
    expect(skillColor('a')).toMatch(/^#[0-9a-f]{6}$/)
  })
})
