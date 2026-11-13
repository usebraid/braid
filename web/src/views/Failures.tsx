// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { Link } from 'react-router-dom'
import { useApi } from '../api/hooks'
import { useMe } from '../api/session'
import { statsFailures } from '../api/stats'
import type { FailureItem } from '../api/types'
import { useFilters } from '../api/useFilters'
import { BarList } from '../components/charts/BarList'
import { StreamChart } from '../components/charts/StreamChart'
import { FilterBar } from '../components/FilterBar'
import { Card, Empty, ErrorNote, KpiCard, Pill } from '../components/ui'
import { categoryLabel, categoryTone, limitLabel } from '../lib/failures'
import { dayLabel, fmtDateTime, fmtLost } from '../lib/format'
import { useInfinite } from '../lib/useInfinite'

const PALETTE = ['var(--p1)', 'var(--p2)', 'var(--p3)', 'var(--p4)', 'var(--p5)', 'var(--p6)', 'var(--p7)', 'var(--p8)']
const on = (e: { type: string }) => e.type === 'session_updated' || e.type === 'reindex'
const aggText = (a: { count: number; time_lost_s: number }) =>
  a.time_lost_s > 0 ? `${a.count} · ${fmtLost(a.time_lost_s)}` : String(a.count)

export function Failures() {
  const me = useMe()
  const { query } = useFilters()
  const key = JSON.stringify(query)
  // the aggregates ride on every page; one tiny page is enough for them
  const sum = useApi(`failures:${key}`, (s) => statsFailures({ ...query, limit: 1 }, s), { on })
  const list = useInfinite<FailureItem>({
    key,
    id: (f) => `${f.session_id}:${f.agent_id}:${f.line_id}:${f.occurred_at}`,
    load: (cursor, signal) => statsFailures({ ...query, cursor, limit: 50 }, signal),
    refreshOn: on,
  })
  const d = sum.data
  const total = d?.by_category.reduce((a, c) => a + c.count, 0) ?? 0
  const top = d?.by_category.reduce((a, c) => (c.count > a.count ? c : a), { key: '', count: 0, time_lost_s: 0 })
  return (
    <>
      <div className="top">
        <h1>Failures</h1>
      </div>
      <FilterBar />
      {sum.error && !d ? <ErrorNote error={sum.error} retry={sum.reload} /> : null}
      {d ? (
        <>
          <div className="kpis kpis-minor">
            <KpiCard small label="Failures" value={String(total)} colors={['var(--p1)', 'var(--p2)']} />
            <KpiCard small label="Time lost" value={fmtLost(d.time_lost_s)} colors={['var(--p3)', 'var(--p4)']} />
            <KpiCard
              small
              label="Most frequent"
              value={top?.count ? `${categoryLabel(top.key)} ×${top.count}` : '–'}
              colors={['var(--p5)', 'var(--p6)']}
            />
          </div>
          <Card>
            <h2>Failures per day</h2>
            <StreamChart
              title="Failures per day"
              points={d.by_day.map((x) => ({ key: x.key, label: dayLabel(x.key), value: x.count }))}
              format={String}
              colors={['var(--p5)', 'var(--p6)']}
              empty="No failures in this period"
            />
          </Card>
          <div className="grid2">
            <Card>
              <h2>By category</h2>
              <BarList
                title="By category"
                rows={d.by_category.map((c, i) => ({
                  key: c.key,
                  label: categoryLabel(c.key),
                  value: c.count,
                  text: aggText(c),
                  color: PALETTE[i % PALETTE.length] as string,
                }))}
              />
            </Card>
            <Card>
              <h2>By limit type</h2>
              <BarList
                title="By limit type"
                rows={d.by_limit_type.map((c, i) => ({
                  key: c.key,
                  label: limitLabel(c.key),
                  value: c.count,
                  text: aggText(c),
                  color: PALETTE[i % PALETTE.length] as string,
                }))}
              />
            </Card>
          </div>
        </>
      ) : null}
      {list.error ? <ErrorNote error={list.error} retry={list.more} /> : null}
      {list.items.length === 0 && !list.loading && !list.error ? (
        <Empty>No failures in this period</Empty>
      ) : (
        <Card>
          <ul className="rows">
            {list.items.map((f) => (
              <li key={`${f.session_id}:${f.agent_id}:${f.line_id}:${f.occurred_at}`}>
                <div className="row-head">
                  {/* one text per pill so the category label stays unique on the page */}
                  <Pill kind={categoryTone(f.category)}>
                    {f.limit_type
                      ? `${categoryLabel(f.category)} · ${limitLabel(f.limit_type)}`
                      : categoryLabel(f.category)}
                  </Pill>
                  <span className="sub num">{fmtDateTime(f.occurred_at, me.time_zone)}</span>
                  {f.resets_at ? <span className="sub">resets {fmtDateTime(f.resets_at, me.time_zone)}</span> : null}
                  {f.time_lost_s ? <span className="sub">lost {fmtLost(f.time_lost_s)}</span> : null}
                  <Link className="sub" to={`/sessions/${f.session_id}?agent=${f.agent_id}&line=${f.line_id}`}>
                    Jump to position
                  </Link>
                </div>
                {f.detail ? <p className="row-body">{f.detail}</p> : null}
              </li>
            ))}
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
