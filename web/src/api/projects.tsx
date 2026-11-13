// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { createContext, type ReactNode, useContext } from 'react'
import { toQuery } from '../lib/filters'
import { useApi } from './hooks'
import { statsFilters } from './stats'
import type { FilterOptions } from './types'

type Project = FilterOptions['projects'][number]
const Ctx = createContext<Project[]>([])

// Every project of all time with its session count, for the sidebar and the project colours.
export function ProjectsProvider({ children }: { children: ReactNode }) {
  const { data } = useApi('projects:all', (s) => statsFilters(toQuery({ range: 'all' }), s), {
    on: (e) => e.type === 'session_updated' || e.type === 'reindex',
  })
  return <Ctx.Provider value={data?.projects ?? []}>{children}</Ctx.Provider>
}

export const useProjects = () => useContext(Ctx)

// A project keeps its palette colour wherever it shows: its place in the all-time list (sorted by path), or a hash of
// the path while that list is loading or lacks it.
export function projectColor(projects: Project[], path: string): string {
  const i = projects.findIndex((p) => p.path === path)
  let h = 0
  for (const c of path) h = (h * 31 + c.charCodeAt(0)) >>> 0
  return `var(--p${((i < 0 ? h : i) % 8) + 1})`
}
