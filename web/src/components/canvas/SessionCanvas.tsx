// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { Background, Controls, MiniMap, ReactFlow } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { useMemo } from 'react'
import type { SessionDetail } from '../../api/types'
import { type AgentNodeData, buildGraph, skillColor } from '../../lib/graph'
import { getTheme } from '../../theme/theme'
import { AgentNode } from './AgentNode'

const nodeTypes = { agent: AgentNode }

export function SessionCanvas({ detail, liveAgentIds }: { detail: SessionDetail; liveAgentIds: Set<string> }) {
  // ids are stable keys, so React Flow keeps the viewport when a live update replaces the data
  const { nodes, edges } = useMemo(() => buildGraph(detail, { liveAgentIds }), [detail, liveAgentIds])
  return (
    <div className="card canvas">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        nodesDraggable={false}
        nodesConnectable={false}
        nodesFocusable={false}
        elementsSelectable
        fitView
        minZoom={0.2}
        colorMode={getTheme()}
        aria-label="Agent graph"
      >
        <Background />
        <Controls />
        <MiniMap pannable zoomable nodeColor={(n) => skillColor((n.data as AgentNodeData).agent.agent_type)} />
      </ReactFlow>
    </div>
  )
}
