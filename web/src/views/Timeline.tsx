// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { useSearchParams } from 'react-router-dom'
import { useApi } from '../api/hooks'
import { useMe } from '../api/session'
import { listDevices, statsSessions } from '../api/stats'
import type { SessionItem } from '../api/types'
import { useFilters } from '../api/useFilters'
import { FilterBar, PageHead } from '../components/FilterBar'
import { TimelineGraph } from '../components/graph/TimelineGraph'
import { Card, Empty, ErrorNote, Segmented, TableWrap } from '../components/ui'
import { useInfinite } from '../lib/useInfinite'
import { SessionRow } from './SessionRow'

export function Timeline() {
  const [sp, setSp] = useSearchParams()
  const view = sp.get('view') === 'list' ? 'list' : 'graph'
  const setView = (next: 'graph' | 'list') =>
    setSp(
      (prev) => {
        const n = new URLSearchParams(prev)
        if (next === 'graph') n.delete('view')
        else n.set('view', next)
        return n
      },
      { replace: true },
    )
  return (
    <>
      <PageHead title="Timeline" range />
      <FilterBar />
      <div className="sess-tools">
        <Segmented
          label="Timeline view"
          value={view}
          options={[
            { value: 'graph', label: 'Graph' },
            { value: 'list', label: 'List' },
          ]}
          onChange={setView}
        />
      </div>
      {view === 'list' ? <TimelineList /> : <TimelineGraph />}
    </>
  )
}

function TimelineList() {
  const me = useMe()
  const { query } = useFilters()
  const devs = useApi('devices', listDevices)
  const names = new Map((Array.isArray(devs.data) ? devs.data : []).map((d) => [d.id, d.name]))
  const list = useInfinite<SessionItem>({
    key: JSON.stringify(query),
    id: (s) => s.session_id,
    load: (cursor, signal) => statsSessions({ ...query, cursor, limit: 50 }, signal),
    refreshOn: (e) => e.type === 'session_updated',
  })
  return (
    <>
      {list.error ? <ErrorNote error={list.error} retry={list.more} /> : null}
      {list.items.length === 0 && !list.loading && !list.error ? (
        <Empty>No sessions in this period. Pick a longer range or reset the filters.</Empty>
      ) : (
        <Card>
          <TableWrap label="Sessions">
            <table>
              <thead>
                <tr>
                  <th>Session</th>
                  <th>Models</th>
                  <th className="r">Agent time</th>
                  <th className="r">Cost</th>
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
          </TableWrap>
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
