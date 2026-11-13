// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { useCallback } from 'react'
import { useSearchParams } from 'react-router-dom'

// The selected agent lives in `?agent=`; changing it drops `line`, which only makes sense for one agent.
export function useAgentParam(): [string | null, (id: string | null) => void] {
  const [sp, setSp] = useSearchParams()
  const set = useCallback(
    (id: string | null) =>
      setSp((prev) => {
        const next = new URLSearchParams(prev)
        if (id) next.set('agent', id)
        else next.delete('agent')
        next.delete('line')
        return next
      }),
    [setSp],
  )
  return [sp.get('agent'), set]
}
