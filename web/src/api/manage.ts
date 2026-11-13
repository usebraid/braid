// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { api } from './client'
import type { Device, Me } from './types'

const enc = encodeURIComponent

export const renameDevice = (id: string, name: string) =>
  api<Device>('PATCH', `/api/v1/devices/${enc(id)}`, { body: { name } })
export const revokeDevice = (id: string) => api<void>('DELETE', `/api/v1/devices/${enc(id)}`)
export const setTimeZone = (time_zone: string) => api<Me>('PATCH', '/api/v1/me', { body: { time_zone } })
