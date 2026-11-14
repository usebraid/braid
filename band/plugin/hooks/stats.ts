// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors
// Adapted from ponytail's band (hooks/register.tsx, MIT, Copyright (c) 2026 DietrichGebert): the palette names
// (success, warning, error, inactive, suggestion, dim) and the "one row of Text segments" idea. See THIRD_PARTY_NOTICES.md.

import type { Stats, SyncState } from '../types'

// The engine's $.session.usage() has no token total, so the module passes the cost only.
export type Local = { cost: number; tokens?: number } | undefined
export type Color = 'success' | 'warning' | 'error' | 'inactive'
export type Segment = { text: string; color?: Color; dim?: boolean }
export type View = { kind: 'hidden' } | { kind: 'not_connected'; text: string } | { kind: 'line'; segments: Segment[] }

export const STATS_TTL_MS = 30_000
const STATES: readonly string[] = ['ok', 'backlog', 'blocked', 'offline', 'logged_out']
const isNum = (v: unknown) => typeof v === 'number' && Number.isFinite(v)

export function parseStats(stdout: string): Stats | undefined {
  let v: unknown
  try {
    v = JSON.parse(stdout)
  } catch {
    return undefined
  }
  const s = v as Partial<Stats> | null
  if (typeof s !== 'object' || s === null || s.version !== 1) return undefined
  if (!s.sync || !STATES.includes(s.sync.state) || !isNum(s.sync.backlog_bytes)) return undefined
  const t = s.today
  if (!t || !isNum(t.cost_usd) || !isNum(t.tokens) || !isNum(t.sessions)) return undefined
  return s as Stats
}

export function dotColor(s: SyncState): Color {
  return s === 'ok' ? 'success' : s === 'backlog' ? 'warning' : 'error'
}

export function fmtMoney(usd: number): string {
  return usd > 0 && usd < 0.01 ? '<$0.01' : `$${usd.toFixed(2)}`
}

export function fmtTokens(n: number): string {
  if (n >= 999_950) return `${(n / 1e6).toFixed(1)}M`
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`
  return String(Math.round(n))
}

export function countdown(resetsAt: string, nowMs: number): string | undefined {
  const ms = Date.parse(resetsAt) - nowMs
  if (!(ms > 0)) return undefined
  const m = Math.max(1, Math.ceil(ms / 60_000))
  return m < 60 ? `resets in ${m}m` : `resets in ${Math.floor(m / 60)}h ${String(m % 60).padStart(2, '0')}m`
}

export function due(lastRunMs: number | undefined, nowMs: number, force: boolean): boolean {
  return force || lastRunMs === undefined || nowMs < lastRunMs || nowMs - lastRunMs >= STATS_TTL_MS
}

const bytes = (n: number) =>
  n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} MB` : n >= 1024 ? `${Math.round(n / 1024)} KB` : `${n} B`

export function view(stats: Stats | undefined, local: Local, nowMs: number, stale = false): View {
  if (!stats || stats.sync.state === 'logged_out') return { kind: 'not_connected', text: 'braid: not connected' }
  const { state } = stats.sync
  const old = stale || state === 'offline'
  const segs: Segment[] = []
  const mine = local ?? (stats.session && { cost: stats.session.cost_usd, tokens: stats.session.tokens })
  if (mine) {
    const text = mine.tokens === undefined ? fmtMoney(mine.cost) : `${fmtMoney(mine.cost)} · ${fmtTokens(mine.tokens)}`
    segs.push({ text }, { text: '│', dim: true })
  }
  const { today } = stats
  const n = today.sessions
  segs.push({
    text: `today ${fmtMoney(today.cost_usd)} · ${fmtTokens(today.tokens)} · ${n} ${n === 1 ? 'session' : 'sessions'}`,
    ...(old && { dim: true }),
  })
  segs.push({ text: '●', color: dotColor(state) })
  const reset = stats.limit && countdown(stats.limit.resets_at, nowMs)
  if (reset) segs.push({ text: reset, color: 'warning' })
  if (old) segs.push({ text: 'stale', dim: true })
  if (state === 'backlog') segs.push({ text: `${bytes(stats.sync.backlog_bytes)} behind`, dim: true })
  if (state === 'blocked') segs.push({ text: 'blocked', color: 'error' })
  return { kind: 'line', segments: segs }
}
