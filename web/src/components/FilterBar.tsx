// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { get } from '../api/client'
import { useApi } from '../api/hooks'
import type { Device, FilterOptions } from '../api/types'
import { useFilters } from '../api/useFilters'
import { RANGES, type RangeId } from '../lib/filters'
import { modelName } from '../lib/format'
import { Segmented } from './ui'

type Show = { range?: boolean; device?: boolean; project?: boolean; model?: boolean; skill?: boolean }

function Select(p: {
  label: string
  all: string
  value: string
  options: { value: string; label: string }[]
  onChange: (v: string) => void
}) {
  return (
    <label className="field">
      <span className="sub">{p.label}</span>
      <select value={p.value} onChange={(e) => p.onChange(e.target.value)}>
        <option value="">{p.all}</option>
        {p.options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
    </label>
  )
}

export function FilterBar({ show = {} }: { show?: Show }) {
  const { filters, query, set, reset } = useFilters()
  const on = { range: true, device: true, project: true, model: true, skill: true, ...show }
  const devices = useApi('devices', (s) => get<Device[]>('/api/v1/devices', undefined, s))
  const opts = useApi(`filters:${query.from}:${query.to}`, (s) =>
    get<FilterOptions>('/api/v1/stats/filters', { from: query.from, to: query.to }, s),
  )
  const custom = Boolean(filters.from && filters.to)
  const range: RangeId | '' = custom ? '' : (filters.range ?? '30d')
  const list = Array.isArray(devices.data) ? devices.data : []
  const o = opts.data
  return (
    <div className="filters">
      {on.range ? (
        <>
          <Segmented<RangeId | ''>
            label="Time range"
            value={range}
            options={RANGES.map((r) => ({ value: r.id, label: r.id === 'all' ? 'All' : r.id }))}
            onChange={(v) => v && set('range', v)}
          />
          {custom ? <span className="sub">Custom range</span> : null}
        </>
      ) : null}
      {on.device ? (
        <Select
          label="Device"
          all="All devices"
          value={filters.device ?? ''}
          options={list.map((d) => ({ value: d.id, label: d.name }))}
          onChange={(v) => set('device', v)}
        />
      ) : null}
      {on.project ? (
        <Select
          label="Project"
          all="All projects"
          value={filters.project ?? ''}
          options={(o?.projects ?? []).map((p) => ({ value: p.path, label: p.name }))}
          onChange={(v) => set('project', v)}
        />
      ) : null}
      {on.model ? (
        <Select
          label="Model"
          all="All models"
          value={filters.model ?? ''}
          options={(o?.models ?? []).map((m) => ({ value: m, label: modelName(m) }))}
          onChange={(v) => set('model', v)}
        />
      ) : null}
      {on.skill ? (
        <Select
          label="Skill"
          all="All skills"
          value={filters.skill ?? ''}
          options={(o?.skills ?? []).map((s) => ({ value: s, label: s }))}
          onChange={(v) => set('skill', v)}
        />
      ) : null}
      <button type="button" className="btn" onClick={reset}>
        Reset
      </button>
    </div>
  )
}
