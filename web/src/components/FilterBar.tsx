// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { useApi } from '../api/hooks'
import { listDevices, statsFilters } from '../api/stats'
import { useFilters } from '../api/useFilters'
import { RANGES, type RangeId } from '../lib/filters'
import { modelName } from '../lib/format'
import { Segmented, Select } from './ui'

type Show = { range?: boolean; device?: boolean; project?: boolean; model?: boolean; skill?: boolean }

export function FilterBar({ show = {} }: { show?: Show }) {
  const { filters, query, set, reset } = useFilters()
  const on = { range: true, device: true, project: true, model: true, skill: true, ...show }
  const devices = useApi('devices', listDevices)
  const opts = useApi(`filters:${query.from}:${query.to}`, (s) => statsFilters({ from: query.from, to: query.to }, s))
  const custom = Boolean(filters.from && filters.to)
  const range: RangeId | '' = custom ? '' : (filters.range ?? '30d')
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
      {on.range ? (
        <>
          <Segmented<RangeId | ''>
            label="Time range"
            value={range}
            options={RANGES.map((r) => ({ value: r.id, label: r.id === 'all' ? 'All' : r.id }))}
            onChange={(v) => v && set('range', v)}
          />
          {custom ? <span className="custom">Custom range</span> : null}
        </>
      ) : null}
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
