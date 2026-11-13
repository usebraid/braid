// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { get, type Query } from './client'
import type {
  ChatPage,
  Device,
  FailurePage,
  FilterOptions,
  LinePage,
  LivePage,
  Overview,
  SearchResult,
  SessionDetail,
  SessionPage,
  TimelinePage,
} from './types'

export const listDevices = (signal?: AbortSignal) => get<Device[]>('/api/v1/devices', undefined, signal)
export const statsFilters = (q: Query, signal?: AbortSignal) => get<FilterOptions>('/api/v1/stats/filters', q, signal)
export const statsOverview = (q: Query, signal?: AbortSignal) => get<Overview>('/api/v1/stats/overview', q, signal)
export const statsSessions = (q: Query, signal?: AbortSignal) => get<SessionPage>('/api/v1/stats/sessions', q, signal)

const enc = encodeURIComponent
export const statsSession = (id: string, signal?: AbortSignal) =>
  get<SessionDetail>(`/api/v1/stats/sessions/${enc(id)}`, undefined, signal)
export const statsSessionLines = (id: string, q: Query, signal?: AbortSignal) =>
  get<LinePage>(`/api/v1/stats/sessions/${enc(id)}/lines`, q, signal)
export const statsLive = (signal?: AbortSignal) => get<LivePage>('/api/v1/stats/live', undefined, signal)
export const statsFailures = (q: Query, signal?: AbortSignal) => get<FailurePage>('/api/v1/stats/failures', q, signal)
export const statsSearch = (q: Query, signal?: AbortSignal) => get<SearchResult>('/api/v1/stats/search', q, signal)
export const statsSessionChat = (id: string, q: Query, signal?: AbortSignal) =>
  get<ChatPage>(`/api/v1/stats/sessions/${enc(id)}/chat`, q, signal)
/** @public used by the timeline graph, which lands in the next change */
export const statsTimeline = (q: Query, signal?: AbortSignal) => get<TimelinePage>('/api/v1/stats/timeline', q, signal)
