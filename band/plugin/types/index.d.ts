// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors
// Adapted from ponytail's hooks/band/types.d.ts (MIT, Copyright (c) 2026 DietrichGebert).
export type SyncState = 'ok' | 'backlog' | 'blocked' | 'offline' | 'logged_out'
export type Stats = {
  version: 1
  sync: { state: SyncState; backlog_bytes: number; last_ack?: string }
  today: { cost_usd: number; tokens: number; sessions: number; agent_time_s: number; failures: number }
  session?: { cost_usd: number; tokens: number; failures: number }
  limit?: { type: string; resets_at: string }
}
export type BandState = { stats?: Stats; lastRun?: number; stale?: boolean }

declare module 'claude-code' {
  interface PluginState {
    braid: { band: BandState }
  }
}
