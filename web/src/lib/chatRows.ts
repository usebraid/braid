// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import type { AgentItem, ChatLine } from '../api/types'
import { isBackground } from './lanes'
import { type Entry, entriesOf } from './transcript'

// What a dot on the graph stands for; GraphCell draws it, the legend names it.
export type Dot = 'prompt' | 'reply' | 'ring' | 'fail' | 'skill' | 'bgcmd' | 'notice' | 'interrupt' | 'compaction'

export type Row =
  | { kind: 'entry'; key: string; lane: number; agentId: string; entry: Entry; dot: Dot; skill?: string }
  | {
      kind: 'branch'
      key: string
      lane: number
      from: number
      agentId: string
      label: string
      at: string | null
      open: boolean
      background: boolean
    }
  | {
      kind: 'merge'
      key: string
      lane: number
      into: number
      agentId: string
      at: string | null
      summary: string
      background: boolean
    }

const SUMMARY_MAX = 200

// The graph shows the conversation; bookkeeping lines (events, system subtypes) stay in the List view.
const shown = (e: Entry) => (e.kind !== 'event' && e.kind !== 'system') || e.isError === true

// An API error line has no content blocks, so transcript.ts gives it no entry; the graph shows it as a failure.
function withFailure(l: ChatLine, entries: Entry[]): Entry[] {
  const m = l.line as { isApiErrorMessage?: unknown; error?: unknown; timestamp?: unknown } | null
  if (entries.length > 0 || typeof m !== 'object' || m === null || m.isApiErrorMessage !== true) return entries
  const at = typeof m.timestamp === 'string' ? m.timestamp : null
  const text = `API error${typeof m.error === 'string' ? `: ${m.error}` : ''}`
  return [{ key: `${l.id}:err`, lineId: l.id, at, kind: 'system', text, truncated: false, isError: true }]
}

const skillOf = (e: Entry) => {
  const i = e.tool?.input as { skill?: unknown } | undefined
  return e.tool?.name === 'Skill' && typeof i?.skill === 'string' ? i.skill : undefined
}

function dotOf(e: Entry, bgCmds: Set<string>): Dot {
  if (e.isError) return 'fail'
  switch (e.kind) {
    case 'prompt':
    case 'reply':
    case 'compaction':
    case 'interrupt':
      return e.kind
    case 'tool_call':
      return e.background ? 'bgcmd' : skillOf(e) ? 'skill' : 'ring'
    case 'notification':
      return bgCmds.has(e.tool?.id ?? '') ? 'bgcmd' : 'notice'
    default:
      return 'ring'
  }
}

export function buildRows(
  lines: ChatLine[],
  agents: AgentItem[],
  lanes: Map<string, number>,
  collapsed: Set<string>,
): Row[] {
  const lane = (id: string) => lanes.get(id) ?? 0
  const bySpawn = new Map<string, AgentItem>()
  const byId = new Map<string, AgentItem>()
  for (const a of agents) {
    byId.set(a.agent_id, a)
    if (a.spawn_tool_use_id) bySpawn.set(a.spawn_tool_use_id, a)
  }
  // a notification names its agent by the spawning tool call (queue) or by the sender's id (hand-back)
  const agentOf = (e: Entry) => (e.tool?.id ? bySpawn.get(e.tool.id) : e.from ? byId.get(e.from) : undefined)

  const perLine = lines.map((l) => ({ l, entries: withFailure(l, entriesOf(l)) }))
  const bgCmds = new Set<string>() // background commands: their notification gets the same dot
  for (const { entries } of perLine) for (const e of entries) if (e.background && e.tool) bgCmds.add(e.tool.id)
  const steps = new Map<string, number>()
  for (const { l, entries } of perLine)
    steps.set(l.agent_id, (steps.get(l.agent_id) ?? 0) + entries.filter(shown).length)

  // A background agent returns with its notification; its tool result is only the launch ack.
  const returns = (e: Entry, a: AgentItem | undefined) =>
    a !== undefined && e.kind === (isBackground(a) ? 'notification' : 'tool_result')
  const resolved = new Set<string>()
  for (const { entries } of perLine)
    for (const e of entries) {
      const a = e.kind === 'notification' ? agentOf(e) : e.tool ? bySpawn.get(e.tool.id) : undefined
      if (a && returns(e, a)) resolved.add(a.agent_id)
    }
  const stepsOf = (id: string) => {
    const n = steps.get(id) ?? 0
    return `${n} ${n === 1 ? 'step' : 'steps'}`
  }

  const rows: Row[] = []
  const merged = new Set<string>()
  const noted = new Set<string>() // notified tool ids of background commands: the queue line and the user line say it once
  for (const { l, entries } of perLine) {
    if (collapsed.has(l.agent_id)) continue
    for (const entry of entries) {
      const agent = entry.kind === 'notification' ? agentOf(entry) : entry.tool ? bySpawn.get(entry.tool.id) : undefined
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
          open: !agent.merged_at && !resolved.has(agent.agent_id),
          background: isBackground(agent),
        })
      } else if (agent && entry.kind === 'notification') {
        // the first notification of an agent that returns by one is its merge, whichever lane its line sits in (queue lines
        // are in the main file, also for a background agent spawned by a subagent); a repeat, or one of a foreground agent, adds nothing
        if (!returns(entry, agent) || merged.has(agent.agent_id)) continue
        merged.add(agent.agent_id)
        rows.push({
          kind: 'merge',
          key: `merge:${entry.key}`,
          lane: lane(agent.agent_id),
          into: lane(parentId),
          agentId: agent.agent_id,
          at: entry.at ?? l.ts,
          summary: collapsed.has(agent.agent_id) ? stepsOf(agent.agent_id) : entry.text.slice(0, SUMMARY_MAX),
          background: isBackground(agent),
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
          background: isBackground(agent),
        })
      } else if (shown(entry)) {
        if (entry.kind === 'notification' && entry.tool?.id) {
          if (noted.has(entry.tool.id)) continue
          noted.add(entry.tool.id)
        }
        rows.push({
          kind: 'entry',
          key: entry.key,
          lane: lane(l.agent_id),
          agentId: l.agent_id,
          entry,
          dot: dotOf(entry, bgCmds),
          skill: skillOf(entry),
        })
      }
    }
  }
  // The merge row follows the agent's last row: notification lines can be stamped before its final lines.
  const last = new Map<string, number>()
  rows.forEach((r, i) => {
    if (r.kind === 'entry') last.set(r.agentId, i)
  })
  const later = new Map<number, Row[]>()
  const out: Row[] = []
  rows.forEach((r, i) => {
    const to = r.kind === 'merge' ? (last.get(r.agentId) ?? -1) : -1
    if (to > i) {
      const q = later.get(to) ?? []
      q.push(r)
      later.set(to, q)
    } else out.push(r)
    out.push(...(later.get(i) ?? []))
  })
  return out
}
