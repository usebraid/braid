// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import type { ReactNode } from 'react'
import { useApi } from '../api/hooks'
import { listDevices, statsFilters } from '../api/stats'
import { useFilters } from '../api/useFilters'
import { RANGES, type RangeId } from '../lib/filters'
import { modelName } from '../lib/format'
import { Segmented, Select } from './ui'

type Show = { device?: boolean; project?: boolean; model?: boolean; skill?: boolean }

// The time range, top right of a page as on ponytail's board; a custom from/to from a link shows as a note instead.
export function RangeSeg() {
  const { filters, set } = useFilters()
  const custom = Boolean(filters.from && filters.to)
  return (
    <div className="actions">
      {custom ? <span className="custom">Custom range</span> : null}
      <Segmented<RangeId | ''>
        label="Time range"
        value={custom ? '' : (filters.range ?? '30d')}
        options={RANGES.map((r) => ({ value: r.id, label: r.id === 'all' ? 'All' : r.id }))}
        onChange={(v) => v && set('range', v)}
      />
    </div>
  )
}

// A page header: optional crumb, the title and a subtitle on the left, the range (or other actions) on the right.
export function PageHead(p: {
  title: string
  crumb?: ReactNode
  sub?: ReactNode
  range?: boolean
  actions?: ReactNode
}) {
  return (
    <div className="top">
      <div>
        {p.crumb ? <div className="crumb">{p.crumb}</div> : null}
        <h1>{p.title}</h1>
        {p.sub ? <p>{p.sub}</p> : null}
      </div>
      {p.range ? <RangeSeg /> : p.actions}
    </div>
  )
}

// Device, project, model and skill: a quiet row under the header.
export function FilterBar({ show = {} }: { show?: Show }) {
  const { filters, query, set, reset } = useFilters()
  const on = { device: true, project: true, model: true, skill: true, ...show }
  const devices = useApi('devices', listDevices)
  const opts = useApi(`filters:${query.from}:${query.to}`, (s) => statsFilters({ from: query.from, to: query.to }, s))
  const custom = Boolean(filters.from && filters.to)
  const list = Array.isArray(devices.data) ? devices.data : []
  const o = opts.data
  const active = Boolean(
    custom ||
      (filters.range && filters.range !== '30d') ||
      filters.device ||
      filters.project ||
      filters.model ||
      filters.skill,
  )
  return (
    <div className="filters">
      {on.device ? (
        <Select
          label="Device"
          value={filters.device ?? ''}
          options={[{ value: '', label: 'All devices' }, ...list.map((d) => ({ value: d.id, label: d.name }))]}
          onChange={(v) => set('device', v)}
        />
      ) : null}
      {on.project ? (
        <Select
          label="Project"
          value={filters.project ?? ''}
          options={[
            { value: '', label: 'All projects' },
            ...(o?.projects ?? []).map((p) => ({ value: p.path, label: p.name })),
          ]}
          onChange={(v) => set('project', v)}
        />
      ) : null}
      {on.model ? (
        <Select
          label="Model"
          value={filters.model ?? ''}
          options={[
            { value: '', label: 'All models' },
            ...(o?.models ?? []).map((m) => ({ value: m, label: modelName(m) })),
          ]}
          onChange={(v) => set('model', v)}
        />
      ) : null}
      {on.skill ? (
        <Select
          label="Skill"
          value={filters.skill ?? ''}
          options={[{ value: '', label: 'All skills' }, ...(o?.skills ?? []).map((s) => ({ value: s, label: s }))]}
          onChange={(v) => set('skill', v)}
        />
      ) : null}
      {active ? (
        <button type="button" className="btn ghost" onClick={reset}>
          Reset
        </button>
      ) : null}
    </div>
  )
}
