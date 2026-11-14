// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import type { AgentItem, ChatLine } from '../api/types'
import { type Entry, entriesOf } from './transcript'

export type Row =
  | { kind: 'entry'; key: string; lane: number; agentId: string; entry: Entry }
  | {
      kind: 'branch'
      key: string
      lane: number
      from: number
      agentId: string
      label: string
      at: string | null
      open: boolean
    }
  | {
      kind: 'merge'
      key: string
      lane: number
      into: number
      agentId: string
      at: string | null
      summary: string
    }

const SUMMARY_MAX = 200

export function buildRows(
  lines: ChatLine[],
  agents: AgentItem[],
  lanes: Map<string, number>,
  collapsed: Set<string>,
): Row[] {
  const lane = (id: string) => lanes.get(id) ?? 0
  const bySpawn = new Map<string, AgentItem>()
  for (const a of agents) if (a.spawn_tool_use_id) bySpawn.set(a.spawn_tool_use_id, a)

  const perLine = lines.map((l) => ({ l, entries: entriesOf(l) }))
  const steps = new Map<string, number>()
  for (const { l, entries } of perLine) steps.set(l.agent_id, (steps.get(l.agent_id) ?? 0) + entries.length)

  // A background agent returns with its task notification; its tool result is only the launch ack.
  const returns = (e: Entry, a: AgentItem | undefined) => e.kind === (a?.background ? 'event' : 'tool_result')
  const resolved = new Set<string>()
  for (const { entries } of perLine)
    for (const e of entries) if (e.tool && returns(e, bySpawn.get(e.tool.id))) resolved.add(e.tool.id)
  const stepsOf = (id: string) => {
    const n = steps.get(id) ?? 0
    return `${n} ${n === 1 ? 'step' : 'steps'}`
  }

  const rows: Row[] = []
  for (const { l, entries } of perLine) {
    if (collapsed.has(l.agent_id)) continue
    for (const entry of entries) {
      const agent = entry.tool ? bySpawn.get(entry.tool.id) : undefined
      const parentId = agent?.parent_agent_id ?? 'main'
      if (agent && entry.kind === 'tool_call' && l.agent_id === parentId) {
        const label = agent.description || agent.agent_type
        rows.push({
          kind: 'branch',
          key: `branch:${entry.key}`,
          lane: lane(agent.agent_id),
          from: lane(parentId),
          agentId: agent.agent_id,
          label: collapsed.has(agent.agent_id) ? `${label} (${stepsOf(agent.agent_id)})` : label,
          at: entry.at ?? l.ts,
          open: !agent.merged_at && !resolved.has(entry.tool?.id ?? ''),
        })
      } else if (agent && returns(entry, agent) && l.agent_id === parentId) {
        rows.push({
          kind: 'merge',
          key: `merge:${entry.key}`,
          lane: lane(agent.agent_id),
          into: lane(parentId),
          agentId: agent.agent_id,
          at: entry.at ?? l.ts,
          summary: collapsed.has(agent.agent_id) ? stepsOf(agent.agent_id) : entry.text.slice(0, SUMMARY_MAX),
        })
      } else {
        rows.push({ kind: 'entry', key: entry.key, lane: lane(l.agent_id), agentId: l.agent_id, entry })
      }
    }
  }
  return rows
}
