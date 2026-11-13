// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { createContext, type ReactNode, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import { ErrorNote } from '../components/ui'
import { get } from './client'
import type { Me } from './types'

type MeValue = Me & { refresh: () => void }
const MeContext = createContext<MeValue | null>(null)

// Fetches the signed-in user once; `initial` skips the request (tests).
export function MeProvider({ children, initial }: { children: ReactNode; initial?: Me }) {
  const [me, setMe] = useState<Me | null>(initial ?? null)
  const [error, setError] = useState<unknown>(null)
  const [attempt, setAttempt] = useState(0)
  useEffect(() => {
    if (initial) return
    void attempt
    const ctl = new AbortController()
    get<Me>('/api/v1/me', undefined, ctl.signal).then(setMe, (e: unknown) => {
      if (!ctl.signal.aborted) setError(e)
    })
    return () => ctl.abort()
  }, [initial, attempt])
  const retry = useCallback(() => {
    setError(null)
    setAttempt((n) => n + 1)
  }, [])
  const refresh = useCallback(() => {
    get<Me>('/api/v1/me').then(setMe, () => {}) // keep the current user if the refetch fails
  }, [])
  const value = useMemo(() => (me ? { ...me, refresh } : null), [me, refresh])
  if (error) return <ErrorNote error={error} retry={retry} />
  if (!value) return <p className="empty-note">Loading…</p>
  return <MeContext.Provider value={value}>{children}</MeContext.Provider>
}

export function useMe(): MeValue {
  const me = useContext(MeContext)
  if (!me) throw new Error('useMe outside MeProvider')
  return me
}
