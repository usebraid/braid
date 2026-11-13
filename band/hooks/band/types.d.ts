// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors
// Adapted from ponytail's hooks/band/types.d.ts (MIT, Copyright (c) 2026 DietrichGebert).
import type { Stats } from './stats'

export type BandState = { stats?: Stats; lastRun?: number; stale?: boolean }

declare module 'claude-code' {
  interface PluginState {
    braid: { band: BandState }
  }
}
