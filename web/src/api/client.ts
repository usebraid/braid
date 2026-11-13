// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
  ) {
    super(`${status} ${code}`)
    this.name = 'ApiError'
  }
}

export type Query = Record<string, string | number | boolean | undefined | null>

export async function api<T>(
  method: 'GET' | 'POST' | 'PATCH' | 'DELETE',
  path: string,
  o: { query?: Query; body?: unknown; signal?: AbortSignal } = {},
): Promise<T> {
  const params = new URLSearchParams()
  for (const [k, v] of Object.entries(o.query ?? {})) {
    if (v !== undefined && v !== null && v !== '') params.set(k, String(v))
  }
  const qs = params.toString()
  const init: RequestInit = { method, credentials: 'same-origin', signal: o.signal }
  if (o.body !== undefined) {
    init.body = JSON.stringify(o.body)
    init.headers = { 'Content-Type': 'application/json' }
  }
  const res = await fetch(qs ? `${path}?${qs}` : path, init)
  if (res.status === 401) {
    window.location.assign(`/auth/login?next=${encodeURIComponent(window.location.pathname + window.location.search)}`)
  }
  if (!res.ok) {
    const body = (await res.json().catch(() => null)) as { error?: string } | null
    throw new ApiError(res.status, body?.error ?? 'error')
  }
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

export const get = <T>(path: string, query?: Query, signal?: AbortSignal): Promise<T> =>
  api<T>('GET', path, { query, signal })
