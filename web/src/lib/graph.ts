// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import dagre from '@dagrejs/dagre'
import type { AgentItem, FailureItem, PhaseItem, SessionDetail } from '../api/types'

// Local stand-ins for the React Flow types (Task 19 swaps them for the library's).
export type GNode<D> = {
  id: string
  type: 'agent'
  position: { x: number; y: number }
  data: D
  width: number
  height: number
}
export type GEdge = {
  id: string
  source: string
  target: string
  type: 'smoothstep'
  label?: string
  data?: { orphan: true }
  style?: { strokeDasharray: string }
}

export const NODE_WIDTH = 264
export const phaseHeight = 24
const BASE_HEIGHT = 84

export type AgentNodeData = { agent: AgentItem; phases: PhaseItem[]; failures: FailureItem[]; live: boolean }

const PALETTE = ['#4cc3ff', '#2fd4c0', '#5fe08a', '#ffd166', '#ff8a3d', '#ff6fb5', '#b26bff', '#7c8cff']

export function skillColor(skill: string): string {
  let h = 0
  for (const c of skill) h = (h * 31 + c.charCodeAt(0)) >>> 0
  return PALETTE[h % PALETTE.length] as string
}

const byStart = (a: AgentItem, b: AgentItem) =>
  (a.started_at ?? '').localeCompare(b.started_at ?? '') || a.agent_id.localeCompare(b.agent_id)

export function buildGraph(
  d: SessionDetail,
  o: { liveAgentIds?: Set<string> } = {},
): { nodes: GNode<AgentNodeData>[]; edges: GEdge[] } {
  const agents = [...d.agents].sort(byStart)
  const known = new Set(agents.map((a) => a.agent_id))
  const nodes: GNode<AgentNodeData>[] = agents.map((agent) => {
    const phases = d.skill_phases.filter((p) => p.agent_id === agent.agent_id)
    return {
      id: agent.agent_id,
      type: 'agent',
      position: { x: 0, y: 0 },
      width: NODE_WIDTH,
      height: BASE_HEIGHT + phases.length * phaseHeight,
      data: {
        agent,
        phases,
        failures: d.failures.filter((f) => f.agent_id === agent.agent_id),
        live: o.liveAgentIds?.has(agent.agent_id) ?? false,
      },
    }
  })

  const edges: GEdge[] = []
  for (const a of agents) {
    const id = a.agent_id
    const parent = a.parent_agent_id
    if (parent && parent !== id && known.has(parent)) {
      edges.push({
        id: `${parent}>${id}`,
        source: parent,
        target: id,
        type: 'smoothstep',
        label: a.description || a.agent_type,
      })
    } else if (id !== 'main' && known.has('main')) {
      edges.push({
        id: `main>${id}`,
        source: 'main',
        target: id,
        type: 'smoothstep',
        data: { orphan: true },
        style: { strokeDasharray: '6 4' },
      })
    }
  }

  const g = new dagre.graphlib.Graph()
  g.setGraph({ rankdir: 'TB', nodesep: 40, ranksep: 70 })
  g.setDefaultEdgeLabel(() => ({}))
  for (const n of nodes) g.setNode(n.id, { width: n.width, height: n.height })
  for (const e of edges) g.setEdge(e.source, e.target)
  dagre.layout(g)
  for (const n of nodes) {
    const p = g.node(n.id)
    n.position = { x: p.x - n.width / 2, y: p.y - n.height / 2 }
  }
  nodes.sort((a, b) => a.id.localeCompare(b.id))
  edges.sort((a, b) => a.id.localeCompare(b.id))
  return { nodes, edges }
}
