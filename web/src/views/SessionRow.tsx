// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { Link } from 'react-router-dom'
import type { SessionItem } from '../api/types'
import { Pill } from '../components/ui'
import { fmtCost, fmtDateTime, fmtDuration, modelName } from '../lib/format'

export function SessionRow({
  s,
  devices,
  timeZone,
}: {
  s: SessionItem
  devices: Map<string, string>
  timeZone: string
}) {
  const dev = s.device_id ?? s.device_ids[0] ?? ''
  const skills = s.skills.slice(0, 3)
  return (
    <tr>
      <td>
        <Link className="tl-title" to={`/sessions/${s.session_id}`}>
          {s.title}
        </Link>
        <div className="sub">
          {s.project_name} · {devices.get(dev) ?? dev.slice(0, 8)} · {fmtDateTime(s.started_at, timeZone)}
        </div>
      </td>
      <td>
        <div className="chips">
          {s.models.map((m) => (
            <span key={m}>{modelName(m)}</span>
          ))}
        </div>
      </td>
      <td className="num">
        {s.agent_ms_estimated ? '≈ ' : ''}
        {fmtDuration(s.agent_ms)}
      </td>
      <td className="num">{fmtCost(s.cost_usd)}</td>
      <td>
        <div className="chips">
          {skills.map((k) => (
            <span key={k}>{k}</span>
          ))}
          {s.skills.length > 3 ? <span>+{s.skills.length - 3}</span> : null}
        </div>
      </td>
      <td>
        {s.failures > 0 ? <Pill kind="bad">{`${s.failures} ${s.failures === 1 ? 'failure' : 'failures'}`}</Pill> : null}
        {s.is_live ? <Pill kind="ok">live</Pill> : null}
      </td>
    </tr>
  )
}
