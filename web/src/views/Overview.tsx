// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useApi } from '../api/hooks'
import { useMe } from '../api/session'
import { statsOverview } from '../api/stats'
import { useFilters } from '../api/useFilters'
import { BarList } from '../components/charts/BarList'
import { Honeycomb } from '../components/charts/Honeycomb'
import { StreamChart } from '../components/charts/StreamChart'
import { FilterBar } from '../components/FilterBar'
import { Card, Empty, ErrorNote, KpiCard, Segmented } from '../components/ui'
import { dayLabel, fmtCost, fmtDuration, fmtLost, fmtTokens, modelName } from '../lib/format'

type Metric = 'cost' | 'tokens' | 'sessions'
const PALETTE = ['var(--p1)', 'var(--p2)', 'var(--p3)', 'var(--p4)', 'var(--p5)', 'var(--p6)', 'var(--p7)', 'var(--p8)']

export function Overview() {
  const me = useMe()
  const { query } = useFilters()
  const [metric, setMetric] = useState<Metric>('cost')
  const {
    data: ov,
    error,
    loading,
    reload,
  } = useApi(`overview:${JSON.stringify(query)}`, (s) => statsOverview(query, s), {
    on: (e) => e.type === 'session_updated' || e.type === 'reindex',
  })
  const header = (
    <>
      <div className="top">
        <h1>Overview</h1>
      </div>
      <FilterBar />
    </>
  )
  if (error && !ov)
    return (
      <>
        {header}
        <ErrorNote error={error} retry={reload} />
      </>
    )
  if (!ov)
    return (
      <>
        {header}
        <Empty>{loading ? 'Loading…' : 'No data'}</Empty>
      </>
    )

  const k = ov.kpi
  const days = ov.per_day
  const spark = (f: (d: (typeof days)[number]) => number) => days.map(f)
  const split = ov.per_day_by_project
  const fmt = metric === 'cost' ? fmtCost : fmtTokens
  const value = (d: (typeof days)[number]) =>
    metric === 'cost' ? d.cost_usd : metric === 'tokens' ? d.tokens : d.sessions
  const stack = metric !== 'sessions' && split && split.projects.length > 0
  const layers = stack
    ? split.projects.map((p, i) => ({
        key: p.key,
        label: p.name === '_other' || p.key === '_other' ? 'Other' : p.name,
        color: PALETTE[i % PALETTE.length] as string,
        values: split.days.map((d) => (metric === 'cost' ? d.values[i]?.cost_usd : d.values[i]?.tokens) ?? 0),
      }))
    : undefined
  const title = { cost: 'Cost per day', tokens: 'Tokens per day', sessions: 'Sessions per day' }[metric]
  const unknown = k.cost_unknown_models
  return (
    <>
      {header}
      {ov.reindexing ? (
        <div className="card banner" role="status">
          Rebuilding derived data, numbers may be incomplete
        </div>
      ) : null}
      {me.time_zone_set === false ? (
        <p className="hint">
          Times use {ov.time_zone}. Set your time zone in <Link to="/settings">Settings</Link>.
        </p>
      ) : null}
      {k.sessions === 0 ? (
        <Empty>No sessions in this period. Pick a longer range or reset the filters.</Empty>
      ) : (
        <>
          <div className="kpis">
            <KpiCard
              label="Sessions"
              testId="sessions"
              value={String(k.sessions)}
              colors={['#4cc3ff', '#7c8cff']}
              values={spark((d) => d.sessions)}
            />
            <KpiCard
              label="Agent time"
              testId="time"
              value={fmtDuration(k.agent_ms)}
              estimated={k.agent_ms_estimated}
              colors={['#2fd4c0', '#5fe08a']}
              spark
            />
            <KpiCard
              label="Cost"
              testId="cost"
              value={fmtCost(k.cost_usd)}
              note={
                unknown.length ? `excludes ${unknown.length} unpriced model${unknown.length > 1 ? 's' : ''}` : undefined
              }
              title={unknown.length ? unknown.join(', ') : undefined}
              colors={['#ffd166', '#ff8a3d']}
              values={spark((d) => d.cost_usd)}
            />
            <KpiCard
              label="Tokens"
              testId="tokens"
              value={fmtTokens(k.tokens)}
              colors={['#ff6fb5', '#b26bff']}
              values={spark((d) => d.tokens)}
            />
          </div>
          <div className="kpis kpis-minor">
            <KpiCard
              small
              label="Streak"
              testId="streak"
              value={`${k.streak_days} ${k.streak_days === 1 ? 'day' : 'days'}`}
              colors={['#5fe08a', '#2fd4c0']}
            />
            <KpiCard
              small
              label="Failures"
              testId="failures"
              value={String(k.failures)}
              colors={['#ff6b5b', '#ff8a3d']}
            />
            <KpiCard
              small
              label="Time lost"
              testId="lost"
              value={fmtLost(k.time_lost_s)}
              colors={['#f1c96b', '#ffd166']}
            />
          </div>
          <Card>
            <div className="list-head">
              <h2>{title}</h2>
              <Segmented<Metric>
                label="Metric"
                value={metric}
                options={[
                  { value: 'cost', label: 'Cost' },
                  { value: 'tokens', label: 'Tokens' },
                  { value: 'sessions', label: 'Sessions' },
                ]}
                onChange={setMetric}
              />
            </div>
            <StreamChart
              title={title}
              format={metric === 'sessions' ? String : fmt}
              points={days.map((d) => ({ key: d.day, label: dayLabel(d.day), value: value(d) }))}
              layers={layers}
            />
          </Card>
          <div className="grid2">
            <Card>
              <h2>Models</h2>
              <BarList
                title="Cost by model"
                rows={ov.per_model.map((m, i) => ({
                  key: m.model,
                  label: modelName(m.model),
                  value: m.cost_usd,
                  text: `${fmtCost(m.cost_usd)} · ${fmtTokens(m.tokens)}`,
                  color: PALETTE[i % PALETTE.length] as string,
                  note: m.price_unknown ? 'no price' : undefined,
                }))}
              />
            </Card>
            <Card>
              <h2>Projects</h2>
              <BarList
                title="Cost by project"
                rows={ov.per_project.map((p, i) => ({
                  key: p.path,
                  label: p.name,
                  value: p.cost_usd,
                  text: `${fmtCost(p.cost_usd)} · ${fmtTokens(p.tokens)}`,
                  color: PALETTE[i % PALETTE.length] as string,
                }))}
              />
            </Card>
          </div>
          <Card>
            <Honeycomb
              cells={ov.heatmap}
              unit="messages"
              title={`assistant messages by weekday and hour, in ${ov.time_zone}`}
            />
          </Card>
        </>
      )}
    </>
  )
}
