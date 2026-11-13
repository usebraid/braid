// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { createContext, type ReactNode, useContext, useEffect, useMemo, useRef, useState } from 'react'
import { LiveClient, type LiveEvent, type LiveStatus } from '../lib/live'

export type LiveValue = { status: LiveStatus; subscribe: (fn: (e: LiveEvent) => void) => () => void }
export const LiveContext = createContext<LiveValue>({ status: 'open', subscribe: () => () => {} })

export type LiveSource = Pick<LiveClient, 'start' | 'stop' | 'subscribe' | 'onStatus' | 'status'>

const goLogin = () =>
  window.location.assign(`/auth/login?next=${encodeURIComponent(window.location.pathname + window.location.search)}`)

// `client` is injectable for tests; by default one connects to /api/v1/live.
export function LiveProvider({ children, client }: { children: ReactNode; client?: LiveSource }) {
  const [c] = useState<LiveSource>(
    () =>
      client ??
      new LiveClient({
        url: `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/api/v1/live`,
        onAuthLost: goLogin,
      }),
  )
  const [status, setStatus] = useState(c.status)
  useEffect(() => {
    setStatus(c.status)
    const off = c.onStatus(setStatus)
    c.start()
    return () => {
      off()
      c.stop()
    }
  }, [c])
  const value = useMemo<LiveValue>(() => ({ status, subscribe: (f) => c.subscribe(f) }), [c, status])
  return <LiveContext.Provider value={value}>{children}</LiveContext.Provider>
}

export const useLiveStatus = (): LiveStatus => useContext(LiveContext).status

export function useLiveEvents(handler: (e: LiveEvent) => void): void {
  const { subscribe } = useContext(LiveContext)
  const ref = useRef(handler)
  ref.current = handler
  useEffect(() => subscribe((e) => ref.current(e)), [subscribe])
}

export function LiveBanner() {
  return useLiveStatus() === 'paused' ? (
    <div className="live-banner" role="status">
      Live updates paused, reconnecting…
    </div>
  ) : (
    <div role="status" />
  )
}
