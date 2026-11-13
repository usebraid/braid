// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { Handle, type Node, type NodeProps, Position } from '@xyflow/react'
import type { KeyboardEvent } from 'react'
import { fmtCost, fmtDuration, fmtTokens } from '../../lib/format'
import { type AgentNodeData, skillColor } from '../../lib/graph'
import { useAgentParam } from '../../lib/useAgentParam'
import { Pill } from '../ui'

const skillName = (s: string) => s.slice(s.indexOf(':') + 1)

export function AgentNode({ data }: NodeProps<Node<AgentNodeData, 'agent'>>) {
  const { agent: a, phases, failures, live } = data
  const [selected, select] = useAgentParam()
  const calls = Object.values(a.tool_calls).reduce((n, v) => n + v, 0)
  const key = (e: KeyboardEvent) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      select(a.agent_id)
    }
  }
  return (
    // biome-ignore lint/a11y/useSemanticElements: a button cannot hold the block content of the node card
    <div
      className={live ? 'card agent-node live' : 'card agent-node'}
      role="button"
      tabIndex={0}
      data-testid={`agent-node-${a.agent_id}`}
      aria-label={`${a.agent_type} agent ${a.agent_id}`}
      aria-current={selected === a.agent_id ? 'true' : undefined}
      onClick={() => select(a.agent_id)}
      onKeyDown={key}
    >
      <Handle type="target" position={Position.Top} isConnectable={false} />
      <header>
        <span>{a.agent_type}</span>
        <span className="num">
          {a.duration_estimated ? '≈ ' : ''}
          {fmtDuration(a.duration_ms)}
        </span>
      </header>
      <div className="desc">{a.description}</div>
      <div className="counts num">
        <span>{calls} calls</span>
        <span>{a.tool_errors} errors</span>
        <span>{fmtCost(a.usage.cost_usd)}</span>
        <span>{fmtTokens(a.usage.tokens)} tok</span>
      </div>
      {phases.map((p) => (
        <div key={p.seq} className="phase" style={{ background: skillColor(p.skill) }} title={p.skill}>
          {skillName(p.skill)}
        </div>
      ))}
      {failures.length > 0 ? (
        <Pill kind="bad">{`${failures.length} ${failures.length === 1 ? 'failure' : 'failures'}`}</Pill>
      ) : null}
      <Handle type="source" position={Position.Bottom} isConnectable={false} />
    </div>
  )
}
