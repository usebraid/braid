// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// fmtDuration, fmtTokens, fmtCost and modelName are adapted from ponytail
// (https://github.com/DietrichGebert/ponytail, hooks/band/board.ts: time, tok, money, modelName),
// Copyright (c) 2026 DietrichGebert, MIT License; the license text is in THIRD_PARTY_NOTICES.md.

export function fmtDuration(ms: number): string {
  const s = Math.round(ms / 1000)
  if (s < 60) return `${s}s`
  if (s < 3600) return `${Math.floor(s / 60)}m ${s % 60}s`
  const min = Math.round(s / 60)
  return `${Math.floor(min / 60)}h ${min % 60}m`
}

export function fmtTokens(n: number): string {
  if (n < 1000) return String(Math.round(n))
  return n < 1e6 ? `${+(n / 1e3).toFixed(1)}k` : `${+(n / 1e6).toFixed(1)}M`
}

export function fmtCost(usd: number): string {
  if (usd > 0 && usd < 0.005) return '<$0.01'
  return `$${usd.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
}

export function fmtLost(seconds: number): string {
  if (seconds < 60) return `${Math.round(seconds)}s`
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`
  const min = Math.round(seconds / 60)
  return `${Math.floor(min / 60)}h ${min % 60}m`
}

export function modelName(id: string): string {
  const [family = '', ...rest] = id
    .replace(/^claude-/, '')
    .replace(/-\d{8}$/, '')
    .split('-')
  const name = family.charAt(0).toUpperCase() + family.slice(1)
  return rest.length ? `${name} ${rest.join('.')}` : name
}

export function dayKey(iso: string, timeZone: string): string {
  return new Date(iso).toLocaleDateString('en-CA', { timeZone })
}

export function fmtDateTime(iso: string, timeZone: string): string {
  return new Intl.DateTimeFormat('en-US', {
    timeZone,
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).format(new Date(iso))
}

export const dayLabel = (day: string) =>
  new Date(`${day}T00:00:00Z`).toLocaleDateString('en-US', { month: 'short', day: 'numeric', timeZone: 'UTC' })
