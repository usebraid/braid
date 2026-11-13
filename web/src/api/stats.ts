// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { get, type Query } from './client'
import type { Device, FilterOptions, Overview, SessionPage } from './types'

export const listDevices = (signal?: AbortSignal) => get<Device[]>('/api/v1/devices', undefined, signal)
export const statsFilters = (q: Query, signal?: AbortSignal) => get<FilterOptions>('/api/v1/stats/filters', q, signal)
export const statsOverview = (q: Query, signal?: AbortSignal) => get<Overview>('/api/v1/stats/overview', q, signal)
export const statsSessions = (q: Query, signal?: AbortSignal) => get<SessionPage>('/api/v1/stats/sessions', q, signal)
