// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { useMemo } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { useApi } from '../api/hooks'
import { listDevices, statsSession } from '../api/stats'
import type { AgentItem } from '../api/types'
import { SessionCanvas } from '../components/canvas/SessionCanvas'
import { TranscriptPanel } from '../components/TranscriptPanel'
import { Card, Empty, ErrorNote, Pill } from '../components/ui'
import { fmtCost, fmtDuration, modelName } from '../lib/format'
import { buildGraph } from '../lib/graph'
import { useAgentParam } from '../lib/useAgentParam'

export function Session() {
  const id = useParams().id ?? ''
  const [sp] = useSearchParams()
  const [selected, select] = useAgentParam()
  const { data, error, loading, reload } = useApi(`session:${id}`, (s) => statsSession(id, s), {
    on: (e) => e.type === 'session_updated' && e.session_id === id,
  })
  const devs = useApi('devices', listDevices)
  const line = Number(sp.get('line'))
  const focusLine = Number.isInteger(line) && line > 0 ? line : undefined
  const back = new URLSearchParams(sp)
  back.delete('agent')
  back.delete('line')
  const backTo = back.size ? `/timeline?${back}` : '/timeline'
  // a running session has agents that have not reported finished
  const live = useMemo(
    () => new Set(data?.session.is_live ? data.agents.filter((a) => a.finished === false).map((a) => a.agent_id) : []),
    [data],
  )
  const list = useMemo(() => {
    if (!data) return []
    const { nodes, edges } = buildGraph(data)
    const kids = new Map<string, string[]>()
    for (const e of edges) kids.set(e.source, [...(kids.get(e.source) ?? []), e.target])
    const byId = new Map(nodes.map((n) => [n.id, n.data.agent]))
    const out: { id: string; depth: number; agent: AgentItem }[] = []
    const walk = (agentId: string, depth: number) => {
      const agent = byId.get(agentId)
      if (agent) out.push({ id: agentId, depth, agent })
      for (const c of kids.get(agentId) ?? []) walk(c, depth + 1)
    }
    const targets = new Set(edges.map((e) => e.target))
    for (const n of nodes) if (!targets.has(n.id)) walk(n.id, 0)
    return out
  }, [data])

  const backLink = (
    <Link to={backTo} className="crumb">
      ← Timeline
    </Link>
  )
  if (error && !data)
    return (
      <>
        {backLink}
        <ErrorNote error={error} retry={reload} />
      </>
    )
  if (!data)
    return (
      <>
        {backLink}
        <Empty>{loading ? 'Loading…' : 'No data'}</Empty>
      </>
    )
  const s = data.session
  const names = new Map((Array.isArray(devs.data) ? devs.data : []).map((d) => [d.id, d.name]))
  return (
    <>
      <div className="top">
        <div>
          {backLink}
          <h1>{s.title}</h1>
          <div className="sess-meta">
            <span>{s.project_name}</span>
            <span>{s.device_ids.map((d) => names.get(d) ?? d.slice(0, 8)).join(', ')}</span>
            <span>{s.models.map(modelName).join(', ')}</span>
            <span className="num">{fmtDuration(s.agent_ms)}</span>
            <span className="num">{fmtCost(s.cost_usd)}</span>
            {s.is_live ? <Pill kind="ok">live</Pill> : null}
          </div>
        </div>
      </div>
      <div className={selected ? 'sess-body with-panel' : 'sess-body'}>
        <Card className="agent-list">
          <h2>Agents</h2>
          {list.map(({ id: aid, depth, agent }) => (
            <button
              key={aid}
              type="button"
              style={{ paddingLeft: 8 + depth * 14 }}
              aria-current={selected === aid ? 'true' : undefined}
              onClick={() => select(aid)}
            >
              {agent.agent_type} · {agent.description}
            </button>
          ))}
        </Card>
        <SessionCanvas detail={data} liveAgentIds={live} />
        {selected ? (
          <TranscriptPanel key={selected} sessionId={id} agentId={selected} focusLine={focusLine} liveTick={data} />
        ) : null}
      </div>
    </>
  )
}
