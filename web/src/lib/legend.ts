// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import type { SessionDetail } from '../api/types'
import type { Dot, Row } from './chatRows'

export type Shape = 'dot' | 'ring' | 'square' | 'diamond' | 'line' | 'dashed' | 'box' | 'bar'
export type LegendItem = { key: string; label: string; shape: Shape; color: string }

const DOTS: [Dot, LegendItem][] = [
  ['prompt', { key: 'prompt', label: 'Your prompt', shape: 'dot', color: 'var(--focus)' }],
  ['reply', { key: 'reply', label: 'Reply', shape: 'dot', color: 'var(--p1)' }],
  ['ring', { key: 'ring', label: 'Tool call, result or thinking', shape: 'ring', color: 'var(--p1)' }],
  ['fail', { key: 'fail', label: 'Failed tool call', shape: 'ring', color: 'var(--bad)' }],
  ['skill', { key: 'skill', label: 'Skill', shape: 'dot', color: 'var(--p7)' }],
  ['bgcmd', { key: 'bgcmd', label: 'Background command', shape: 'diamond', color: 'var(--bgcmd)' }],
  ['notice', { key: 'notice', label: 'Notification', shape: 'ring', color: 'var(--faint)' }],
  ['interrupt', { key: 'interrupt', label: 'Interrupted by user', shape: 'square', color: 'var(--warn)' }],
  ['compaction', { key: 'compaction', label: 'Compaction', shape: 'dashed', color: 'var(--faint)' }],
]

// Only what the loaded rows show: a legend that lists absent types is noise.
export function graphLegend(rows: Row[]): LegendItem[] {
  const dots = new Set(rows.flatMap((r) => (r.kind === 'entry' ? [r.dot] : [])))
  const sub = rows.some((r) => r.kind === 'branch')
  return [
    ...DOTS.filter(([d]) => dots.has(d)).map(([, i]) => i),
    ...(sub ? [{ key: 'sub', label: 'Subagent', shape: 'line' as const, color: 'var(--p4)' }] : []),
  ]
}

export function canvasLegend(d: SessionDetail): LegendItem[] {
  const subs = d.agents.filter((a) => a.agent_id !== 'main')
  return [
    { key: 'main', label: 'Main session', shape: 'box', color: 'var(--line2)' },
    ...(subs.length ? [{ key: 'sub', label: 'Subagent', shape: 'box' as const, color: 'var(--line2)' }] : []),
    ...(d.skill_phases.length
      ? [{ key: 'phases', label: 'Skill phases', shape: 'bar' as const, color: 'var(--p7)' }]
      : []),
  ]
}
