// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { api, get } from './client'
import type { AdminStats, AuditPage, Device, Me } from './types'

const enc = encodeURIComponent

export const renameDevice = (id: string, name: string) =>
  api<Device>('PATCH', `/api/v1/devices/${enc(id)}`, { body: { name } })
export const revokeDevice = (id: string) => api<void>('DELETE', `/api/v1/devices/${enc(id)}`)
export const setTimeZone = (time_zone: string) => api<Me>('PATCH', '/api/v1/me', { body: { time_zone } })

export const listAudit = (before: number | undefined, signal?: AbortSignal) =>
  get<AuditPage>('/api/v1/audit', { before, limit: 50 }, signal)
export const adminListAudit = (before: number | undefined, signal?: AbortSignal) =>
  get<AuditPage>('/api/v1/admin/audit', { before, limit: 50 }, signal)

export const adminStats = (signal?: AbortSignal) => get<AdminStats>('/api/v1/admin/stats', undefined, signal)
export const disableUser = (id: string) => api<void>('POST', `/api/v1/admin/users/${enc(id)}/disable`)
export const enableUser = (id: string) => api<void>('POST', `/api/v1/admin/users/${enc(id)}/enable`)
export const deleteUser = (id: string, email: string) =>
  api<void>('DELETE', `/api/v1/admin/users/${enc(id)}`, { query: { confirm: email } })
