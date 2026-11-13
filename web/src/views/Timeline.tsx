// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { get } from '../api/client'
import { useApi } from '../api/hooks'
import { useMe } from '../api/session'
import type { Device, SessionItem, SessionPage } from '../api/types'
import { useFilters } from '../api/useFilters'
import { FilterBar } from '../components/FilterBar'
import { Card, Empty, ErrorNote } from '../components/ui'
import { useInfinite } from '../lib/useInfinite'
import { SessionRow } from './SessionRow'

export function Timeline() {
  const me = useMe()
  const { query } = useFilters()
  const devs = useApi('devices', (s) => get<Device[]>('/api/v1/devices', undefined, s))
  const names = new Map((Array.isArray(devs.data) ? devs.data : []).map((d) => [d.id, d.name]))
  const list = useInfinite<SessionItem>({
    key: JSON.stringify(query),
    id: (s) => s.session_id,
    load: (cursor, signal) => get<SessionPage>('/api/v1/stats/sessions', { ...query, cursor, limit: 50 }, signal),
    refreshOn: (e) => e.type === 'session_updated',
  })
  return (
    <>
      <div className="top">
        <h1>Timeline</h1>
      </div>
      <FilterBar />
      {list.error ? <ErrorNote error={list.error} retry={list.more} /> : null}
      {list.items.length === 0 && !list.loading && !list.error ? (
        <Empty>No sessions in this period</Empty>
      ) : (
        <Card>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Session</th>
                  <th>Models</th>
                  <th>Agent time</th>
                  <th>Cost</th>
                  <th>Skills</th>
                  <th>Status</th>
                </tr>
              </thead>
              <tbody>
                {list.items.map((s) => (
                  <SessionRow key={s.session_id} s={s} devices={names} timeZone={me.time_zone} />
                ))}
              </tbody>
            </table>
          </div>
          {list.done ? null : (
            <div className="more" ref={list.sentinel}>
              <button type="button" className="btn" disabled={list.loading} onClick={list.more}>
                Load more
              </button>
            </div>
          )}
        </Card>
      )}
    </>
  )
}
