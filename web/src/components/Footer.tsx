// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { useEffect, useState } from 'react'
import { get } from '../api/client'
import type { About } from '../api/types'

export function Footer() {
  const [about, setAbout] = useState<About | null>(null)
  useEffect(() => {
    const ctl = new AbortController()
    get<About>('/api/v1/about', undefined, ctl.signal).then(setAbout, () => {})
    return () => ctl.abort()
  }, [])
  return (
    <footer className="foot">
      <p>Measured from Claude Code transcripts. Cost is priced at API rates, not a bill.</p>
      {about ? (
        <>
          Braid {about.version} ·{' '}
          <a href={about.source_url} rel="noreferrer">
            Source (AGPL-3.0-or-later)
          </a>{' '}
          ·{' '}
        </>
      ) : null}
      <a href="/third-party-licenses.txt">Third-party licenses</a> ·{' '}
      <span>
        Built on <a href="https://github.com/simple10/agents-observe">agents-observe</a> &amp;{' '}
        <a href="https://github.com/DietrichGebert/ponytail">ponytail</a>
      </span>
    </footer>
  )
}
