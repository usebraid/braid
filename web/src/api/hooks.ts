// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { useCallback, useEffect, useRef, useState } from 'react'
import type { LiveEvent } from '../lib/live'
import { useLiveEvents } from './live'

const DEBOUNCE_MS = 400

export function useApi<T>(
  key: string | null,
  load: (signal: AbortSignal) => Promise<T>,
  o: { on?: (e: LiveEvent) => boolean } = {},
): { data: T | undefined; error: unknown; loading: boolean; reload: () => void } {
  const [data, setData] = useState<T>()
  const [error, setError] = useState<unknown>()
  const [loading, setLoading] = useState(key !== null)
  const [n, setN] = useState(0)
  const loadRef = useRef(load)
  loadRef.current = load
  const onRef = useRef(o.on)
  onRef.current = o.on
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  useEffect(() => {
    void n
    if (key === null) {
      setLoading(false)
      return
    }
    const ctl = new AbortController()
    setLoading(true)
    loadRef.current(ctl.signal).then(
      (d) => {
        if (ctl.signal.aborted) return
        setData(d)
        setError(undefined)
        setLoading(false)
      },
      (e: unknown) => {
        if (ctl.signal.aborted) return
        setError(e)
        setLoading(false)
      },
    )
    return () => ctl.abort()
  }, [key, n])

  useEffect(() => () => clearTimeout(timer.current), [])
  const reload = useCallback(() => setN((v) => v + 1), [])
  useLiveEvents((e) => {
    if (key === null || (e.type !== 'resync' && !onRef.current?.(e))) return
    clearTimeout(timer.current)
    timer.current = setTimeout(reload, DEBOUNCE_MS)
  })
  return { data, error, loading, reload }
}
