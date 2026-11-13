// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { Fragment } from 'react'
import { Link } from 'react-router-dom'
import { useApi } from '../../api/hooks'
import { projectColor, useProjects } from '../../api/projects'
import { useMe } from '../../api/session'
import { listDevices, statsTimeline } from '../../api/stats'
import type { TimelineEvent } from '../../api/types'
import { useFilters } from '../../api/useFilters'
import { dayKey, dayLabel, modelName } from '../../lib/format'
import { useInfinite } from '../../lib/useInfinite'
import { Card, Empty, ErrorNote, Pill } from '../ui'

const clock = (iso: string, timeZone: string) =>
  new Intl.DateTimeFormat('en-US', { timeZone, hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).format(
    new Date(iso),
  )

export function TimelineGraph() {
  const me = useMe()
  const { query } = useFilters()
  const projects = useProjects()
  const devs = useApi('devices', listDevices)
  const names = new Map((Array.isArray(devs.data) ? devs.data : []).map((d) => [d.id, d.name]))
  const list = useInfinite<TimelineEvent>({
    key: JSON.stringify(query),
    id: (e) => `${e.session_id}:${e.kind}:${e.at}`,
    load: async (cursor, signal) => {
      const p = await statsTimeline({ ...query, cursor, limit: 50 }, signal)
      return { items: p.events, next: p.next }
    },
    refreshOn: (e) => e.type === 'session_updated',
  })
  let day = ''
  return (
    <>
      {list.error ? <ErrorNote error={list.error} retry={list.more} /> : null}
      {list.items.length === 0 && !list.loading && !list.error ? (
        <Empty>No sessions in this period. Pick a longer range or reset the filters.</Empty>
      ) : (
        <Card>
          <ul className="tl-graph">
            {list.items.map((e) => {
              const d = dayKey(e.at, me.time_zone)
              const sep = d !== day
              day = d
              const dev = e.device_id ? (names.get(e.device_id) ?? e.device_id.slice(0, 8)) : null
              const meta = [e.project_name, dev, e.model ? modelName(e.model) : null].filter(Boolean).join(' · ')
              return (
                <Fragment key={`${e.session_id}:${e.kind}:${e.at}`}>
                  {sep ? <li className="tl-day">{dayLabel(d)}</li> : null}
                  <li>
                    <Link className="tl-row" to={`/sessions/${e.session_id}`}>
                      <time dateTime={e.at}>{clock(e.at, me.time_zone)}</time>
                      <span
                        className={e.kind === 'start' ? 'tl-dot' : 'tl-dot ring'}
                        style={{ color: projectColor(projects, e.project) }}
                        aria-hidden="true"
                      />
                      <span className="tl-body">
                        <span className="tl-title">{e.title}</span>
                        <span className="pills">
                          {e.failures > 0 ? (
                            <Pill kind="bad">{`${e.failures} ${e.failures === 1 ? 'failure' : 'failures'}`}</Pill>
                          ) : null}
                          {e.is_live ? <Pill kind="ok">live</Pill> : null}
                        </span>
                        <span className="tl-sub">{`${meta} · ${e.kind === 'start' ? 'started' : 'resumed'}`}</span>
                      </span>
                    </Link>
                  </li>
                </Fragment>
              )
            })}
          </ul>
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
