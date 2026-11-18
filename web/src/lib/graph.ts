// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import dagre from '@dagrejs/dagre'
import type { Edge, Node } from '@xyflow/react'
import type { AgentItem, FailureItem, PhaseItem, SessionDetail } from '../api/types'

// width and height are always set here, so they are required (the library types make them optional).
export type GNode<D extends Record<string, unknown>> = Node<D, 'agent' | 'more'> & { width: number; height: number }
export type GEdge = Edge<{ orphan: true }>

export const NODE_WIDTH = 232
export const NODE_HEIGHT = 72
// A parent with more children than this shows the first MAX_CHILDREN - 1 and one "+N more" node.
export const MAX_CHILDREN = 12

export type AgentNodeData = { agent: AgentItem; phases: PhaseItem[]; failures: FailureItem[]; live: boolean }
export type MoreNodeData = { parent: string; hidden: number }

const PALETTE = ['#4cc3ff', '#2fd4c0', '#5fe08a', '#ffd166', '#ff8a3d', '#ff6fb5', '#b26bff', '#7c8cff']

export function skillColor(skill: string): string {
  let h = 0
  for (const c of skill) h = (h * 31 + c.charCodeAt(0)) >>> 0
  return PALETTE[h % PALETTE.length] as string
}

const byStart = (a: AgentItem, b: AgentItem) =>
  (a.started_at ?? '').localeCompare(b.started_at ?? '') || a.agent_id.localeCompare(b.agent_id)

// The spawn tree: each agent's parent (unknown parents fall back to main) and children in start order.
export function agentTree(d: SessionDetail) {
  const agents = [...d.agents].sort(byStart)
  const known = new Set(agents.map((a) => a.agent_id))
  const parentOf = new Map<string, { id: string; orphan: boolean }>()
  const kids = new Map<string, string[]>()
  for (const a of agents) {
    const id = a.agent_id
    const p = a.parent_agent_id
    const link =
      p && p !== id && known.has(p)
        ? { id: p, orphan: false }
        : id !== 'main' && known.has('main')
          ? { id: 'main', orphan: true }
          : null
    if (!link) continue
    parentOf.set(id, link)
    kids.set(link.id, [...(kids.get(link.id) ?? []), id])
  }
  const roots = agents.filter((a) => !parentOf.has(a.agent_id)).map((a) => a.agent_id)
  return { agents, parentOf, kids, roots }
}

export function buildGraph(
  d: SessionDetail,
  o: { liveAgentIds?: Set<string>; expanded?: Set<string> } = {},
): { nodes: GNode<AgentNodeData | MoreNodeData>[]; edges: GEdge[] } {
  const { agents, parentOf, kids, roots } = agentTree(d)
  const byId = new Map(agents.map((a) => [a.agent_id, a]))
  const nodes: GNode<AgentNodeData | MoreNodeData>[] = []
  const edges: GEdge[] = []
  const seen = new Set<string>()
  const visit = (id: string) => {
    if (seen.has(id)) return // a parent cycle in bad data
    seen.add(id)
    const agent = byId.get(id) as AgentItem
    nodes.push({
      id,
      type: 'agent',
      position: { x: 0, y: 0 },
      width: NODE_WIDTH,
      height: NODE_HEIGHT,
      data: {
        agent,
        phases: d.skill_phases.filter((p) => p.agent_id === id),
        failures: d.failures.filter((f) => f.agent_id === id),
        live: o.liveAgentIds?.has(id) ?? false,
      },
    })
    const link = parentOf.get(id)
    if (link)
      edges.push({
        id: `${link.id}>${id}`,
        source: link.id,
        target: id,
        type: 'smoothstep',
        ...(link.orphan ? { data: { orphan: true as const }, style: { strokeDasharray: '6 4' } } : {}),
      })
    const children = kids.get(id) ?? []
    const shown =
      children.length > MAX_CHILDREN && !o.expanded?.has(id) ? children.slice(0, MAX_CHILDREN - 1) : children
    for (const c of shown) visit(c)
    if (shown.length < children.length) {
      const more = `more:${id}`
      nodes.push({
        id: more,
        type: 'more',
        position: { x: 0, y: 0 },
        width: NODE_WIDTH,
        height: NODE_HEIGHT,
        data: { parent: id, hidden: children.length - shown.length },
      })
      edges.push({ id: `${id}>${more}`, source: id, target: more, type: 'smoothstep' })
    }
  }
  for (const r of roots) visit(r)
  // agents caught in a parent cycle reach no root; hidden members of a collapsed group do, and stay hidden
  const reachesRoot = (id: string) => {
    for (let x: string | undefined = id, n = 0; x !== undefined && n <= agents.length; n++) {
      const p: string | undefined = parentOf.get(x)?.id
      if (p === undefined) return true
      x = p
    }
    return false
  }
  for (const a of agents) if (!seen.has(a.agent_id) && !reachesRoot(a.agent_id)) visit(a.agent_id)

  // Left to right: depth runs across, siblings stack vertically, so long sibling runs stay readable.
  const g = new dagre.graphlib.Graph()
  g.setGraph({ rankdir: 'LR', align: 'UL', nodesep: 14, ranksep: 56 })
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
