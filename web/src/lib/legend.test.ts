// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { describe, expect, it } from 'vitest'
import type { AgentItem, SessionDetail } from '../api/types'
import type { Row } from './chatRows'
import { canvasLegend, graphLegend } from './legend'

const entry = (dot: Extract<Row, { kind: 'entry' }>['dot']): Row =>
  ({ kind: 'entry', key: dot, lane: 0, agentId: 'main', dot, entry: {} }) as Row
const branch = (background: boolean): Row => ({ kind: 'branch', background }) as Row

describe('graphLegend', () => {
  it('lists only the types present, in a fixed order', () => {
    expect(graphLegend([entry('reply'), entry('prompt')]).map((i) => i.key)).toEqual(['prompt', 'reply'])
    expect(graphLegend([]).length).toBe(0)
  })
  it('treats background agents as subagents; only background commands get their own type', () => {
    expect(graphLegend([branch(false)]).map((i) => i.key)).toEqual(['sub'])
    expect(graphLegend([branch(true), entry('bgcmd'), entry('fail'), entry('compaction')]).map((i) => i.key)).toEqual([
      'fail',
      'bgcmd',
      'compaction',
      'sub',
    ])
  })
})

describe('canvasLegend', () => {
  const ag = (id: string, background = false) => ({ agent_id: id, background }) as AgentItem
  const d = (agents: AgentItem[]) => ({ agents, skill_phases: [], failures: [] }) as unknown as SessionDetail
  it('lists background agents as subagents', () => {
    expect(canvasLegend(d([ag('main'), ag('b', true)])).map((i) => i.key)).toEqual(['main', 'sub'])
    expect(canvasLegend(d([ag('main')])).map((i) => i.key)).toEqual(['main'])
  })
})
