// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { Background, Controls, MiniMap, ReactFlow, type ReactFlowInstance } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { useMemo, useRef, useState } from 'react'
import type { SessionDetail } from '../../api/types'
import { type AgentNodeData, agentTree, buildGraph, skillColor } from '../../lib/graph'
import { useAgentParam } from '../../lib/useAgentParam'
import { useTheme } from '../../theme/useTheme'
import { AgentNode, MoreNode } from './AgentNode'

const nodeTypes = { agent: AgentNode, more: MoreNode }

export function SessionCanvas({ detail, liveAgentIds }: { detail: SessionDetail; liveAgentIds: Set<string> }) {
  const theme = useTheme()
  const [selected] = useAgentParam()
  const [opened, setOpened] = useState<Set<string>>(() => new Set())
  // a selected agent inside a collapsed group (from a link or the list) opens its ancestors' groups
  const expanded = useMemo(() => {
    const out = new Set(opened)
    const { parentOf } = agentTree(detail)
    for (let id = selected, n = 0; id && n < 64; n++) {
      const p = parentOf.get(id)?.id
      if (p) out.add(p)
      id = p ?? null
    }
    return out
  }, [opened, selected, detail])
  // ids are stable keys, so React Flow keeps the viewport when a live update replaces the data
  const { nodes, edges } = useMemo(() => {
    const g = buildGraph(detail, { liveAgentIds, expanded })
    const onExpand = (id: string) => setOpened((s) => new Set(s).add(id))
    return { ...g, nodes: g.nodes.map((n) => (n.type === 'more' ? { ...n, data: { ...n.data, onExpand } } : n)) }
  }, [detail, liveAgentIds, expanded])
  const box = useRef<HTMLDivElement>(null)
  // The first view never zooms below 1, so 12px node text stays 12px: a graph that fits is centred, a bigger one opens
  // at its top-left (main, with dagre's up-left alignment) and the user pans or uses the minimap for the rest.
  const onInit = (rf: Pick<ReactFlowInstance, 'setViewport'>) => {
    const w = box.current?.clientWidth ?? 800
    const h = box.current?.clientHeight ?? 500
    const right = Math.max(...nodes.map((n) => n.position.x + n.width))
    const bottom = Math.max(...nodes.map((n) => n.position.y + n.height))
    const left = Math.min(...nodes.map((n) => n.position.x))
    const top = Math.min(...nodes.map((n) => n.position.y))
    // each axis is centred when it fits, else starts at the top-left edge
    const at = (lo: number, hi: number, room: number) => (hi - lo + 48 <= room ? (room - (hi - lo)) / 2 - lo : 24 - lo)
    void rf.setViewport({ x: at(left, right, w), y: at(top, bottom, h), zoom: 1 })
  }
  return (
    <div className="card canvas" ref={box}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        nodesDraggable={false}
        nodesConnectable={false}
        nodesFocusable={false}
        elementsSelectable
        onInit={onInit}
        minZoom={0.2}
        colorMode={theme}
        aria-label="Agent graph"
      >
        <Background gap={24} size={1} />
        <Controls showInteractive={false} />
        <MiniMap
          pannable
          zoomable
          nodeBorderRadius={4}
          nodeColor={(n) =>
            n.type === 'agent' ? skillColor((n.data as AgentNodeData).agent.agent_type) : 'var(--faint)'
          }
        />
      </ReactFlow>
    </div>
  )
}
