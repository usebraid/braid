// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// The board's structure follows ponytail (https://github.com/DietrichGebert/ponytail, hooks/band/board.ts @96608e0: header,
// KPI tiles with the change against the previous period, the stream by project with session dots and chips, the session
// table and the activity card), Copyright (c) 2026 DietrichGebert, MIT License; the license text is in THIRD_PARTY_NOTICES.md.

import { type CSSProperties, useState } from 'react'
import { Link } from 'react-router-dom'
import { useApi } from '../api/hooks'
import { projectColor, useProjects } from '../api/projects'
import { useMe } from '../api/session'
import { statsOverview, statsSessions } from '../api/stats'
import type { Overview as OverviewData, SessionItem } from '../api/types'
import { useFilters } from '../api/useFilters'
import { Honeycomb } from '../components/charts/Honeycomb'
import { type Dot, type Layer, StreamChart } from '../components/charts/StreamChart'
import { FilterBar, PageHead } from '../components/FilterBar'
import { Chip, Empty, ErrorNote, KpiCard, Pill, Segmented, TableWrap } from '../components/ui'
import { dayRange, daysBetween, fillDays, fromFirst } from '../lib/days'
import {
  COST_NOTE,
  dayLabel,
  fmtCost,
  fmtDuration,
  fmtLost,
  fmtShortDateTime,
  fmtTokens,
  modelName,
} from '../lib/format'
import { useInfinite } from '../lib/useInfinite'

type Metric = 'time' | 'cost' | 'tokens'
type Value = { cost_usd: number; tokens: number; agent_ms: number }
const METRICS: Record<Metric, { label: string; pick: (v: Value) => number; fmt: (n: number) => string }> = {
  time: { label: 'Agent time', pick: (v) => v.agent_ms, fmt: fmtDuration },
  cost: { label: 'Cost', pick: (v) => v.cost_usd, fmt: fmtCost },
  tokens: { label: 'Tokens', pick: (v) => v.tokens, fmt: fmtTokens },
}
const plural = (n: number, w: string) => `${n} ${w}${n === 1 ? '' : 's'}`
const longDate = (day: string) =>
  new Date(`${day}T00:00:00Z`).toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    timeZone: 'UTC',
  })
const change = (v: number, p: number) => (p > 0 ? Math.round(((v - p) / p) * 100) : null)
const live = { on: (e: { type: string }) => e.type === 'session_updated' || e.type === 'reindex' }

export function Overview() {
  const me = useMe()
  const projects = useProjects()
  const { filters, query } = useFilters()
  const {
    data: ov,
    error,
    loading,
    reload,
  } = useApi(`overview:${JSON.stringify(query)}`, (s) => statsOverview(query, s), live)
  const name = filters.project
    ? (projects.find((p) => p.path === filters.project)?.name ??
      ov?.per_project.find((p) => p.path === filters.project)?.name ??
      filters.project)
    : 'All projects'
  const head = (sub?: string) => (
    <>
      <PageHead
        crumb={
          <>
            Overview / <b>{name}</b>
          </>
        }
        title={name}
        sub={sub}
        range
      />
      <FilterBar show={{ project: false }} />
    </>
  )
  if (error && !ov)
    return (
      <>
        {head()}
        <ErrorNote error={error} retry={reload} />
      </>
    )
  if (!ov)
    return (
      <>
        {head()}
        <Empty>{loading ? 'Loading…' : 'No data'}</Empty>
      </>
    )

  const tz = ov.time_zone
  const first = ov.per_day.find((d) => d.sessions > 0)?.day
  const all = filters.range === 'all' && !filters.from
  const range = dayRange(ov.from, ov.to, tz)
  const keys = all ? fromFirst(range, ov.per_day[0]?.day) : range
  const n = keys.length
  const k = ov.kpi
  const sub = k.sessions ? `${plural(k.sessions, 'session')}${first ? ` since ${longDate(first)}` : ''}` : undefined
  return (
    <>
      {head(sub)}
      {ov.reindexing ? (
        <div className="card banner" role="status">
          Rebuilding derived data, numbers may be incomplete
        </div>
      ) : null}
      {me.time_zone_set === false ? (
        <p className="hint">
          Times use {tz}. Set your time zone in <Link to="/settings">Settings</Link>.
        </p>
      ) : null}
      {k.sessions === 0 ? (
        <Empty>No sessions in this period. Pick a longer range or reset the filters.</Empty>
      ) : (
        <Board ov={ov} keys={keys} n={n} all={all} />
      )}
    </>
  )
}

function Board({ ov, keys, n, all }: { ov: OverviewData; keys: string[]; n: number; all: boolean }) {
  const k = ov.kpi
  const prev = ov.previous
  const day = (pick: (d: OverviewData['per_day'][number]) => number) => fillDays(keys, ov.per_day, pick)
  const note = all ? 'all time' : prev.sessions > 0 ? `vs previous ${n} days` : `last ${n} days`
  const delta = (v: number, p: number) => (all ? null : change(v, p))
  const unknown = k.cost_unknown_models
  return (
    <>
      <section className="kpis" aria-label="Totals">
        <KpiCard
          label="Sessions"
          testId="sessions"
          value={String(k.sessions)}
          delta={delta(k.sessions, prev.sessions)}
          note={note}
          colors={['#4cc3ff', '#7c8cff']}
          values={day((d) => d.sessions)}
        />
        <KpiCard
          label="Agent time"
          testId="time"
          value={fmtDuration(k.agent_ms)}
          delta={delta(k.agent_ms, prev.agent_ms)}
          estimated={k.agent_ms_estimated}
          note={note}
          colors={['#2fd4c0', '#5fe08a']}
          values={day((d) => d.agent_ms)}
        />
        <KpiCard
          label="Cost"
          testId="cost"
          value={fmtCost(k.cost_usd)}
          delta={delta(k.cost_usd, prev.cost_usd)}
          note={
            unknown.length ? `${note}, excludes ${unknown.length} unpriced model${unknown.length > 1 ? 's' : ''}` : note
          }
          title={unknown.length ? `${COST_NOTE} Unpriced: ${unknown.join(', ')}` : COST_NOTE}
          colors={['#ffd166', '#ff8a3d']}
          values={day((d) => d.cost_usd)}
        />
        <KpiCard
          label="Tokens"
          testId="tokens"
          value={fmtTokens(k.tokens)}
          delta={delta(k.tokens, prev.tokens)}
          note={note}
          colors={['#ff6fb5', '#b26bff']}
          values={day((d) => d.tokens)}
        />
      </section>
      <Stream ov={ov} keys={keys} title={all ? 'all time' : `last ${n} days`} />
      <div className="grid2">
        <Sessions ov={ov} />
        <Activity ov={ov} />
      </div>
    </>
  )
}

function Stream({ ov, keys, title }: { ov: OverviewData; keys: string[]; title: string }) {
  const projects = useProjects()
  const [metric, setMetric] = useState<Metric>('time')
  const [hidden, setHidden] = useState<Set<string>>(new Set())
  const M = METRICS[metric]
  const split = ov.per_day_by_project
  const color = (key: string) => (key === '_other' ? 'var(--faint)' : projectColor(projects, key))
  const shown = split.projects.map((p, i) => ({ ...p, i, color: color(p.key) }))
  const visible = shown.filter((p) => !hidden.has(p.key))
  const rows = split.days.map((d) => ({ day: d.day, values: d.values }))
  const layers: Layer[] = visible.map((p) => ({
    key: p.key,
    label: p.key === '_other' ? 'Other' : p.name,
    color: p.color,
    values: fillDays(keys, rows, (r) => (r.values[p.i] ? M.pick(r.values[p.i] as Value) : 0)),
  }))
  const totals = keys.map((_, i) => layers.reduce((a, l) => a + (l.values[i] ?? 0), 0))
  const dots: Dot[] = visible.flatMap((p) =>
    fillDays(keys, rows, (r) => r.values[p.i]?.sessions ?? 0).flatMap((c, i) =>
      Array.from({ length: c }, () => ({ i, color: p.color })),
    ),
  )
  const label = `${M.label}, ${title}`
  const toggle = (key: string) =>
    setHidden((h) => {
      const next = new Set(h)
      next.has(key) ? next.delete(key) : next.add(key)
      return next
    })
  return (
    <section className="card chart-card" aria-label={label}>
      <div className="chart-head">
        <div>
          <div className="label">{label}</div>
          <div className="big num">{M.fmt(totals.reduce((a, v) => a + v, 0))}</div>
        </div>
        <Segmented<Metric>
          label="Metric"
          value={metric}
          options={[
            { value: 'time', label: 'Agent time' },
            { value: 'cost', label: 'Cost' },
            { value: 'tokens', label: 'Tokens' },
          ]}
          onChange={setMetric}
        />
      </div>
      <StreamChart
        title={label}
        format={M.fmt}
        points={keys.map((key, i) => ({ key, label: dayLabel(key), value: totals[i] ?? 0 }))}
        layers={layers}
        dots={dots}
      />
      {shown.length > 1 ? (
        <div className="legend">
          {shown.map((p) => (
            <Chip key={p.key} pressed={!hidden.has(p.key)} onClick={() => toggle(p.key)} color={p.color}>
              {p.key === '_other' ? 'Other' : p.name}
              <span className="x" aria-hidden="true">
                {hidden.has(p.key) ? '+' : '×'}
              </span>
            </Chip>
          ))}
        </div>
      ) : null}
    </section>
  )
}

function Sessions({ ov }: { ov: OverviewData }) {
  const me = useMe()
  const projects = useProjects()
  const { filters, query } = useFilters()
  const [q, setQ] = useState('')
  const list = useInfinite<SessionItem>({
    key: JSON.stringify(query),
    id: (s) => s.session_id,
    load: (cursor, signal) => statsSessions({ ...query, cursor, limit: 50 }, signal),
    refreshOn: (e) => e.type === 'session_updated',
  })
  const needle = q.trim().toLowerCase()
  const rows = list.items.filter(
    (s) =>
      !needle || `${s.title} ${s.project_name} ${s.models.map(modelName).join(' ')}`.toLowerCase().includes(needle),
  )
  const max = Math.max(1, ...rows.map((s) => s.agent_ms))
  return (
    <section className="card list" aria-labelledby="sessions-title">
      <div className="list-head">
        <h3 id="sessions-title">
          Sessions<span className="count">{ov.kpi.sessions}</span>
        </h3>
        <label className="search">
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            aria-hidden="true"
          >
            <circle cx="11" cy="11" r="7" />
            <path d="m20 20-3.5-3.5" />
          </svg>
          <input
            type="search"
            placeholder="Search sessions"
            aria-label="Search sessions"
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
        </label>
      </div>
      {list.error ? <ErrorNote error={list.error} retry={list.more} /> : null}
      <TableWrap label="Sessions">
        <table>
          <thead>
            <tr>
              <th>Session</th>
              <th className="r">Agent time</th>
              <th className="r">Cost</th>
              <th className="r">Tokens</th>
              <th className="r">Turns</th>
              <th>Failures</th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 ? (
              <tr>
                <td className="empty" colSpan={6}>
                  {list.loading ? 'Loading…' : needle ? 'No session matches.' : 'No sessions in this period.'}
                </td>
              </tr>
            ) : (
              rows.map((s) => (
                <tr key={s.session_id}>
                  <td className="name">
                    <Link className="tl-title task" to={`/sessions/${s.session_id}`} title={s.title}>
                      {s.title || 'Untitled session'}
                    </Link>
                    <div className="sub">
                      {[
                        filters.project ? '' : s.project_name,
                        fmtShortDateTime(s.started_at, me.time_zone),
                        s.models.map(modelName).join(', '),
                      ]
                        .filter(Boolean)
                        .join(' · ')}
                      {s.is_live ? <Pill kind="ok">live</Pill> : null}
                    </div>
                    <div
                      className="bar"
                      style={
                        {
                          '--c': projectColor(projects, s.project),
                          width: `${Math.max(4, (s.agent_ms / max) * 100)}%`,
                        } as CSSProperties
                      }
                    />
                  </td>
                  <td className="r num">
                    {s.agent_ms_estimated ? <span className="kpi-est">est. </span> : null}
                    {fmtDuration(s.agent_ms)}
                  </td>
                  <td className="r num">{fmtCost(s.cost_usd)}</td>
                  <td className="r num">{fmtTokens(s.tokens)}</td>
                  <td className="r num">{s.turns}</td>
                  <td>
                    {s.failures > 0 ? <Pill kind="bad">{String(s.failures)}</Pill> : <span className="sub">–</span>}
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </TableWrap>
      {list.done ? null : (
        <div className="more">
          <button type="button" className="btn" disabled={list.loading} onClick={list.more}>
            Load more
          </button>
        </div>
      )}
    </section>
  )
}

function Activity({ ov }: { ov: OverviewData }) {
  const k = ov.kpi
  const end = dayRange(ov.from, ov.to, ov.time_zone).at(-1) ?? ''
  const weeks = daysBetween(new Date(Date.parse(`${end}T00:00:00Z`) - 83 * 864e5).toISOString().slice(0, 10), end)
  const vals = fillDays(weeks, ov.activity, (d) => d.agent_ms)
  const active = vals.filter((v) => v > 0).length
  let run = 0
  let longest = 0
  for (const v of vals) {
    run = v > 0 ? run + 1 : 0
    longest = Math.max(longest, run)
  }
  let streak = 0
  for (let j = (vals.at(-1) ?? 0) > 0 ? vals.length - 1 : vals.length - 2; j >= 0 && (vals[j] ?? 0) > 0; j--) streak++
  const busiest = ov.per_day.reduce<OverviewData['per_day'][number] | undefined>(
    (b, d) => (d.agent_ms > (b?.agent_ms ?? 0) ? d : b),
    undefined,
  )
  const facts: [string, string, string?][] = [
    ['Busiest day', busiest ? `${dayLabel(busiest.day)} · ${fmtDuration(busiest.agent_ms)}` : '–'],
    ['Longest session', k.longest_agent_ms ? fmtDuration(k.longest_agent_ms) : '–'],
    ['Average per session', k.sessions ? fmtDuration(k.agent_ms / k.sessions) : '–'],
    ['Average cost per session', k.sessions ? fmtCost(k.cost_usd / k.sessions) : '–'],
    ['Failures', String(k.failures), 'failures'],
    ['Time lost to limits', fmtLost(k.time_lost_s), 'lost'],
  ]
  return (
    <section className="card heat-card" aria-labelledby="activity-title">
      <h3 id="activity-title">Activity</h3>
      <div className="sub">Agent time per day, last 12 weeks</div>
      <Honeycomb
        keys={weeks}
        values={vals}
        title="Agent time per day, last 12 weeks"
        label={dayLabel}
        format={fmtDuration}
      />
      <div className="heroes">
        <div>
          <div className="hero-label" style={{ '--c': '#5fe08a' } as CSSProperties}>
            <i aria-hidden="true">✦</i>Active days
          </div>
          <div className="hero-value num">{active}</div>
          <div className="hero-sub">
            <b>{Math.round((active / 84) * 100)}%</b>of the last 84 days
          </div>
        </div>
        <div>
          <div className="hero-label" style={{ '--c': '#ffd166' } as CSSProperties}>
            <i aria-hidden="true">◆</i>Streak
          </div>
          <div className="hero-value num">{plural(streak, 'day')}</div>
          <div className="hero-sub">longest {plural(longest, 'day')}</div>
        </div>
      </div>
      <dl className="facts">
        {facts.map(([label, value, id]) => (
          <div key={label} className="fact" data-testid={id && `fact-${id}`}>
            <dt>{label}</dt>
            <dd className="num">{value}</dd>
          </div>
        ))}
      </dl>
    </section>
  )
}
