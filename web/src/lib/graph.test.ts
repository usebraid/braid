// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// web/src/lib/graph.test.ts
import { describe, expect, it } from 'vitest'
import type { AgentItem, SessionDetail } from '../api/types'
import { buildGraph, NODE_WIDTH, phaseHeight, skillColor } from './graph'

const usage = { cost_usd: 0, tokens: 0, input: 0, output: 0, cache_read: 0, cache_write: 0, messages: 0 }
const agent = (id: string, parent: string | null, over: Partial<AgentItem> = {}): AgentItem => ({
  agent_id: id,
  agent_type: id === 'main' ? 'main' : 'Explore',
  description: `d-${id}`,
  parent_agent_id: parent,
  spawn_tool_use_id: null,
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
  it('has one node per agent and spawn edges labelled with the description', () => {
    expect(g.nodes.map((n) => n.id)).toEqual(['a1', 'a2', 'a3', 'main'])
    expect(g.edges.map((e) => [e.source, e.target, e.label]).sort()).toEqual([
      ['a1', 'a3', 'd-a3'],
      ['main', 'a1', 'd-a1'],
      ['main', 'a2', 'd-a2'],
    ])
  })
  it('lays out top to bottom: parents above children, siblings on one rank, no overlap', () => {
    expect(node('main').position.y).toBeLessThan(node('a1').position.y)
    expect(node('a1').position.y).toBeLessThan(node('a3').position.y)
    expect(node('a1').position.y).toBe(node('a2').position.y)
    expect(node('a1').position.x).not.toBe(node('a2').position.x)
    expect(Math.abs(node('a1').position.x - node('a2').position.x)).toBeGreaterThanOrEqual(NODE_WIDTH)
  })
  it('grows nodes with their skill phases and carries phases and failures in data', () => {
    expect(node('main').height).toBeGreaterThan(node('a2').height)
    expect(node('main').height - node('a2').height).toBe(2 * phaseHeight)
    expect(node('main').data.phases.map((p) => p.skill)).toEqual([
      'superpowers:brainstorming',
      'superpowers:writing-plans',
    ])
    expect(node('a2').data.failures).toHaveLength(1)
  })
  it('is deterministic', () => {
    expect(JSON.stringify(buildGraph(detail))).toBe(JSON.stringify(g))
  })
  it('attaches an orphan agent to main with a dashed unlabeled edge', () => {
    const o = buildGraph({ ...detail, agents: [agent('main', null), agent('x', null)] })
    expect(o.edges).toEqual([expect.objectContaining({ source: 'main', target: 'x', data: { orphan: true } })])
  })
  it('colours skills stably', () => {
    expect(skillColor('superpowers:brainstorming')).toBe(skillColor('superpowers:brainstorming'))
    expect(skillColor('a')).toMatch(/^#[0-9a-f]{6}$/)
  })
})
