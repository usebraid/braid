// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import { type Filters, parseFilters, toQuery, withFilter } from '../lib/filters'

export function useFilters(): {
  filters: Filters
  query: Record<string, string>
  set: (key: keyof Filters, value: string) => void
  reset: () => void
} {
  const [sp, setSp] = useSearchParams()
  const search = sp.toString()
  // memoised per URL so relative ranges resolve once per change, not on every render
  // biome-ignore lint/correctness/useExhaustiveDependencies: `search` is the key, sp is derived from it
  const filters = useMemo(() => parseFilters(sp), [search])
  // biome-ignore lint/correctness/useExhaustiveDependencies: `filters` is derived from `search`
  const query = useMemo(() => toQuery(filters), [search])
  const set = useCallback((key: keyof Filters, value: string) => setSp((prev) => withFilter(prev, key, value)), [setSp])
  const reset = useCallback(() => setSp(new URLSearchParams()), [setSp])
  return { filters, query, set, reset }
}
