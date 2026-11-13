// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

export type RouteDef = { path: string; label: string; adminOnly?: true }

export const ROUTES: RouteDef[] = [
  { path: '/', label: 'Overview' },
  { path: '/timeline', label: 'Timeline' },
  { path: '/live', label: 'Live' },
  { path: '/failures', label: 'Failures' },
  { path: '/search', label: 'Search' },
  { path: '/devices', label: 'Devices' },
  { path: '/audit', label: 'Audit' },
  { path: '/admin', label: 'Admin', adminOnly: true },
  { path: '/settings', label: 'Settings' },
] // plus /sessions/:id (not in the nav)

export function titleFor(pathname: string): string {
  if (pathname.startsWith('/sessions/')) return 'Session'
  return ROUTES.find((r) => r.path === pathname)?.label ?? 'Not found'
}
