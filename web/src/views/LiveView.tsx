// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useApi } from '../api/hooks'
import { useLiveStatus } from '../api/live'
import { listDevices, statsLive } from '../api/stats'
import { Card, Empty, ErrorNote } from '../components/ui'
import { fmtDuration } from '../lib/format'

const STALE_MS = 2 * 60 * 1000
const ago = (iso: string | null, now: number) => (iso ? `${fmtDuration(Math.max(0, now - Date.parse(iso)))} ago` : '')

export function LiveView() {
  const status = useLiveStatus()
  const { data, error, reload } = useApi('live', statsLive, {
    on: (e) => e.type === 'session_updated' || e.type === 'device_synced' || e.type === 'reindex',
  })
  const devs = useApi('devices', listDevices)
  const names = new Map((Array.isArray(devs.data) ? devs.data : []).map((d) => [d.id, d.name]))
  const [now, setNow] = useState(Date.now)
  useEffect(() => {
    const tick = setInterval(() => setNow(Date.now()), 1000)
    const poll = setInterval(reload, 30_000)
    return () => {
      clearInterval(tick)
      clearInterval(poll)
    }
  }, [reload])
  const items = (data?.items ?? []).filter((x) => now - Date.parse(x.session.ended_at) <= STALE_MS)
  return (
    <>
      <div className="top">
        <h1>Live</h1>
        <span className="sub">{status === 'paused' ? 'Live paused' : 'Receiving live updates'}</span>
      </div>
      {error && !data ? <ErrorNote error={error} retry={reload} /> : null}
      {data && items.length === 0 ? <Empty>No session is running right now</Empty> : null}
      {items.map(({ session: s, agents }) => {
        const dev = s.device_id ?? ''
        return (
          <Card key={s.session_id}>
            <Link className="tl-title" to={`/sessions/${s.session_id}`}>
              {s.title}
            </Link>
            <div className="sub">
              {s.project_name} · {names.get(dev) ?? dev.slice(0, 8)} · last activity{' '}
              <span data-testid="live-last-activity">{ago(s.ended_at, now)}</span>
            </div>
            <ul className="live-agents">
              {agents.map((a) => (
                <li key={a.agent_id}>
                  <span className="pill">{a.agent_type}</span> {a.description}{' '}
                  {a.current_tool ? (
                    <span title={a.current_tool.summary}>
                      {a.current_tool.name} {a.current_tool.summary.slice(0, 80)} · started{' '}
                      {ago(a.current_tool.started_at, now)}
                    </span>
                  ) : (
                    <span className="sub">idle</span>
                  )}
                </li>
              ))}
            </ul>
          </Card>
        )
      })}
    </>
  )
}
