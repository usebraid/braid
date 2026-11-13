// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { type CSSProperties, useMemo } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { useApi } from '../api/hooks'
import { listDevices, statsSession } from '../api/stats'
import type { AgentItem } from '../api/types'
import { SessionCanvas } from '../components/canvas/SessionCanvas'
import { TranscriptPanel } from '../components/TranscriptPanel'
import { Card, Empty, ErrorNote, Pill, Segmented } from '../components/ui'
import { COST_NOTE, fmtCost, fmtDuration, modelName } from '../lib/format'
import { agentTree, skillColor } from '../lib/graph'
import { useAgentParam } from '../lib/useAgentParam'

export function Session() {
  const id = useParams().id ?? ''
  const [sp, setSp] = useSearchParams()
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
  back.delete('view')
  const backTo = back.size ? `/timeline?${back}` : '/timeline'
  // a running session has agents that have not reported finished
  const live = useMemo(
    () => new Set(data?.session.is_live ? data.agents.filter((a) => a.finished === false).map((a) => a.agent_id) : []),
    [data],
  )
  const list = useMemo(() => {
    if (!data) return []
    const { agents, kids, roots } = agentTree(data)
    const byId = new Map(agents.map((a) => [a.agent_id, a]))
    const out: { depth: number; agent: AgentItem }[] = []
    const seen = new Set<string>()
    const walk = (agentId: string, depth: number) => {
      const agent = byId.get(agentId)
      if (!agent || seen.has(agentId)) return
      seen.add(agentId)
      out.push({ depth, agent })
      for (const c of kids.get(agentId) ?? []) walk(c, depth + 1)
    }
    for (const r of roots) walk(r, 0)
    for (const a of agents) walk(a.agent_id, 0) // agents caught in a parent cycle
    return out
  }, [data])
  const view = sp.get('view') === 'list' ? 'list' : 'canvas'
  const setView = (v: 'canvas' | 'list') =>
    setSp(
      (prev) => {
        const n = new URLSearchParams(prev)
        if (v === 'list') n.set('view', 'list')
        else n.delete('view')
        return n
      },
      { replace: true },
    )

  const backLink = (
    <Link to={backTo} className="crumb">
      Back to timeline
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
            <span className="num" title={COST_NOTE}>
              {fmtCost(s.cost_usd)}
            </span>
            {s.is_live ? <Pill kind="ok">live</Pill> : null}
          </div>
        </div>
      </div>
      <div className="sess-tools">
        <span className="sub">{`${list.length} ${list.length === 1 ? 'agent' : 'agents'}`}</span>
        <Segmented
          label="Agent view"
          value={view}
          options={[
            { value: 'canvas', label: 'Canvas' },
            { value: 'list', label: 'List' },
          ]}
          onChange={setView}
        />
      </div>
      <div className={selected ? 'sess-body with-panel' : 'sess-body'}>
        {view === 'list' ? (
          <Card className="tree-card">
            <ul className="agent-tree" aria-label="Agents">
              {list.map(({ depth, agent: a }) => (
                <li key={a.agent_id}>
                  <button
                    type="button"
                    style={{ '--depth': depth, '--c': skillColor(a.agent_type) } as CSSProperties}
                    aria-current={selected === a.agent_id ? 'true' : undefined}
                    onClick={() => select(a.agent_id)}
                  >
                    <span className="dot" />
                    <span className="t">{a.agent_type}</span>
                    <span className="d" title={a.description}>
                      {a.description}
                    </span>
                    <span className="num">{fmtDuration(a.duration_ms)}</span>
                    <span className="num">{fmtCost(a.usage.cost_usd)}</span>
                  </button>
                </li>
              ))}
            </ul>
          </Card>
        ) : (
          <SessionCanvas detail={data} liveAgentIds={live} />
        )}
        {selected ? (
          <TranscriptPanel key={selected} sessionId={id} agentId={selected} focusLine={focusLine} liveTick={data} />
        ) : null}
      </div>
    </>
  )
}
