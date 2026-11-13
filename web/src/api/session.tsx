// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { createContext, type ReactNode, useCallback, useContext, useEffect, useState } from 'react'
import { ErrorNote } from '../components/ui'
import { get } from './client'
import type { Me } from './types'

const MeContext = createContext<Me | null>(null)

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
  if (error) return <ErrorNote error={error} retry={retry} />
  if (!me) return <p className="empty-note">Loading…</p>
  return <MeContext.Provider value={me}>{children}</MeContext.Provider>
}

export function useMe(): Me {
  const me = useContext(MeContext)
  if (!me) throw new Error('useMe outside MeProvider')
  return me
}
