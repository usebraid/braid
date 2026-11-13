// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { Handle, type Node, type NodeProps, Position } from '@xyflow/react'
import type { CSSProperties, KeyboardEvent } from 'react'
import { fmtCost, fmtDuration } from '../../lib/format'
import { type AgentNodeData, type MoreNodeData, skillColor } from '../../lib/graph'
import { useAgentParam } from '../../lib/useAgentParam'

const activate = (fn: () => void) => (e: KeyboardEvent) => {
  if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault()
    fn()
  }
}

const agentState = (a: AgentNodeData) => (a.live ? 'running' : a.failures.length > 0 ? 'failed' : 'finished')

export function AgentNode({ data }: NodeProps<Node<AgentNodeData, 'agent'>>) {
  const { agent: a, phases, failures, live } = data
  const [selected, select] = useAgentParam()
  const state = agentState(data)
  const skills = phases.map((p) => p.skill.slice(p.skill.indexOf(':') + 1))
  // the full text lives in the tooltip and the transcript panel; the card truncates
  const title = [a.description, skills.length ? `Skills: ${skills.join(', ')}` : ''].filter(Boolean).join('\n')
  return (
    // biome-ignore lint/a11y/useSemanticElements: a button cannot hold the block content of the node card
    <div
      className={`agent-node${live ? ' live' : ''}${state === 'failed' ? ' failed' : ''}`}
      style={{ '--c': skillColor(a.agent_type) } as CSSProperties}
      role="button"
      tabIndex={0}
      title={title}
      data-testid={`agent-node-${a.agent_id}`}
      aria-label={`${a.agent_type}: ${a.description || a.agent_id}, ${state}`}
      aria-current={selected === a.agent_id ? 'true' : undefined}
      onClick={() => select(a.agent_id)}
      onKeyDown={activate(() => select(a.agent_id))}
    >
      <Handle type="target" position={Position.Left} isConnectable={false} />
      <header>
        <span className="type">{a.agent_type}</span>
        <span className={`state ${state === 'running' ? 'live' : state === 'failed' ? 'bad' : 'ok'}`} />
      </header>
      <div className="desc">{a.description || (a.agent_id === 'main' ? 'Main conversation' : a.agent_id)}</div>
      <div className="stats">
        <span>{fmtDuration(a.duration_ms)}</span>
        <span>{fmtCost(a.usage.cost_usd)}</span>
        {failures.length > 0 ? (
          <span className="err-n">{`${failures.length} ${failures.length === 1 ? 'failure' : 'failures'}`}</span>
        ) : null}
      </div>
      {phases.length ? (
        <div className="phases" aria-hidden="true">
          {phases.map((p) => (
            <span key={p.seq} style={{ background: skillColor(p.skill) }} />
          ))}
        </div>
      ) : null}
      <Handle type="source" position={Position.Right} isConnectable={false} />
    </div>
  )
}

export function MoreNode({ data }: NodeProps<Node<MoreNodeData & { onExpand: (id: string) => void }, 'more'>>) {
  const open = () => data.onExpand(data.parent)
  return (
    // biome-ignore lint/a11y/useSemanticElements: React Flow positions this div; it acts as a button
    <div
      className="agent-node group"
      role="button"
      tabIndex={0}
      data-testid={`more-${data.parent}`}
      aria-label={`Show ${data.hidden} more agents`}
      onClick={open}
      onKeyDown={activate(open)}
    >
      <Handle type="target" position={Position.Left} isConnectable={false} />
      {`+${data.hidden} more`}
    </div>
  )
}
