// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { useApi } from '../api/hooks'
import { listAudit } from '../api/manage'
import { useMe } from '../api/session'
import { listDevices } from '../api/stats'
import type { AuditEntry, AuditPage } from '../api/types'
import { Card, Empty, ErrorNote } from '../components/ui'
import { fmtDateTime } from '../lib/format'
import { useInfinite } from '../lib/useInfinite'

const humanise = (a: string) => a.replace(/[._]/g, ' ')
const UA_MAX = 40
const detailText = (d: AuditEntry['detail']) =>
  Object.entries(d ?? {})
    .map(([k, v]) => `${k}=${typeof v === 'string' ? v : JSON.stringify(v)}`)
    .join(' ')

// Pages an audit endpoint by the integer id cursor; shared with the admin view.
export function AuditTable(p: {
  load: (before: number | undefined, signal: AbortSignal) => Promise<AuditPage>
  who?: (userId: string | null) => string
  empty?: string
}) {
  const me = useMe()
  const devs = useApi('devices', (s) => listDevices(s))
  const names = new Map((Array.isArray(devs.data) ? devs.data : []).map((d) => [d.id, d.name]))
  const list = useInfinite<Omit<AuditEntry, 'id'> & { id: string }>({
    key: 'audit',
    id: (e) => e.id,
    load: (cursor, signal) =>
      p.load(cursor === null ? undefined : Number(cursor), signal).then((r) => ({
        items: r.entries.map((e) => ({ ...e, id: String(e.id) })),
        next: r.next === null ? null : String(r.next),
      })),
  })
  if (list.error && list.items.length === 0) return <ErrorNote error={list.error} retry={list.more} />
  if (list.items.length === 0 && !list.loading) return <Empty>{p.empty ?? 'No audit entries'}</Empty>
  return (
    <Card>
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Time</th>
              <th>Action</th>
              {p.who ? <th>Actor</th> : null}
              {p.who ? <th>Subject</th> : null}
              <th>Device</th>
              <th>IP</th>
              <th>User agent</th>
              <th>Detail</th>
            </tr>
          </thead>
          <tbody>
            {list.items.map((e) => (
              <tr key={e.id}>
                <td>{fmtDateTime(e.at, me.time_zone)}</td>
                <td title={humanise(e.action)}>{e.action}</td>
                {p.who ? <td>{p.who(e.actor_user_id)}</td> : null}
                {p.who ? <td>{p.who(e.subject_user_id)}</td> : null}
                <td>{e.device_id ? (names.get(e.device_id) ?? e.device_id.slice(0, 8)) : ''}</td>
                <td>{e.ip}</td>
                <td title={e.user_agent ?? undefined}>
                  {e.user_agent && e.user_agent.length > UA_MAX ? `${e.user_agent.slice(0, UA_MAX)}…` : e.user_agent}
                </td>
                <td>{detailText(e.detail)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {list.error ? <ErrorNote error={list.error} retry={list.more} /> : null}
      {list.done ? null : (
        <div className="more" ref={list.sentinel}>
          <button type="button" className="btn" disabled={list.loading} onClick={list.more}>
            Load more
          </button>
        </div>
      )}
    </Card>
  )
}

export function Audit() {
  return (
    <>
      <div className="top">
        <h1>Audit</h1>
      </div>
      <AuditTable load={listAudit} />
    </>
  )
}
