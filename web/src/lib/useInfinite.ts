// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { useCallback, useEffect, useRef, useState } from 'react'
import { useLiveEvents } from '../api/live'
import type { LiveEvent } from './live'

const DEBOUNCE_MS = 400

type Page<T> = { items: T[]; next: string | null }

function merge<T>(front: T[], back: T[], id: (t: T) => string): T[] {
  const seen = new Set(front.map(id))
  return [...front, ...back.filter((t) => !seen.has(id(t)))]
}

export function useInfinite<T>(o: {
  key: string
  id: (t: T) => string
  load: (cursor: string | null, signal: AbortSignal) => Promise<Page<T>>
  refreshOn?: (e: LiveEvent) => boolean
}): {
  items: T[]
  loading: boolean
  error: unknown
  done: boolean
  more: () => void
  sentinel: (el: Element | null) => void
} {
  const [items, setItems] = useState<T[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<unknown>()
  const [done, setDone] = useState(false)
  const next = useRef<string | null>(null)
  const busy = useRef(false)
  const ctl = useRef<AbortController | undefined>(undefined)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const cur = useRef(o)
  cur.current = o

  const fetchPage = useCallback((cursor: string | null, onPage: (p: Page<T>) => void) => {
    const c = ctl.current
    if (!c) return
    busy.current = true
    setLoading(true)
    cur.current.load(cursor, c.signal).then(
      (p) => {
        if (c.signal.aborted) return
        onPage(p)
        setError(undefined)
        busy.current = false
        setLoading(false)
      },
      (e: unknown) => {
        if (c.signal.aborted) return
        setError(e)
        busy.current = false
        setLoading(false)
      },
    )
  }, [])

  // biome-ignore lint/correctness/useExhaustiveDependencies: o.key restarts the list
  useEffect(() => {
    ctl.current = new AbortController()
    next.current = null
    setItems([])
    setDone(false)
    fetchPage(null, (p) => {
      setItems(p.items)
      next.current = p.next
      setDone(p.next === null)
    })
    return () => ctl.current?.abort()
  }, [o.key, fetchPage])

  const more = useCallback(() => {
    if (busy.current || next.current === null) return
    fetchPage(next.current, (p) => {
      setItems((prev) => merge(prev, p.items, cur.current.id))
      next.current = p.next
      setDone(p.next === null)
    })
  }, [fetchPage])

  useEffect(() => () => clearTimeout(timer.current), [])
  useLiveEvents((e) => {
    if (e.type !== 'resync' && !cur.current.refreshOn?.(e)) return
    clearTimeout(timer.current)
    timer.current = setTimeout(() => {
      // shortcut: a refresh while a page load is in flight is skipped, the next event catches up
      if (busy.current) return
      fetchPage(null, (p) => setItems((prev) => merge(p.items, prev, cur.current.id)))
    }, DEBOUNCE_MS)
  })

  const io = useRef<IntersectionObserver | undefined>(undefined)
  const sentinel = useCallback(
    (el: Element | null) => {
      io.current?.disconnect()
      io.current = undefined
      if (!el || typeof IntersectionObserver === 'undefined') return
      io.current = new IntersectionObserver((es) => es.some((x) => x.isIntersecting) && more())
      io.current.observe(el)
    },
    [more],
  )
  return { items, loading, error, done, more, sentinel }
}
