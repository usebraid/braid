// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useApi } from '../api/hooks'
import { useMe } from '../api/session'
import { statsSearch } from '../api/stats'
import { useFilters } from '../api/useFilters'
import { FilterBar, PageHead } from '../components/FilterBar'
import { Card, Empty, ErrorNote } from '../components/ui'
import { fmtDateTime } from '../lib/format'
import { highlight } from '../lib/highlight'

export function Search() {
  const me = useMe()
  const { query } = useFilters()
  const [sp, setSp] = useSearchParams()
  const q = (sp.get('q') ?? '').trim()
  const [text, setText] = useState(sp.get('q') ?? '')
  useEffect(() => {
    const t = setTimeout(
      () =>
        setSp(
          (prev) => {
            const n = new URLSearchParams(prev)
            if (text.trim()) n.set('q', text.trim())
            else n.delete('q')
            return n.toString() === prev.toString() ? prev : n
          },
          { replace: true },
        ),
      300,
    )
    return () => clearTimeout(t)
  }, [text, setSp])
  const ok = q.length >= 2
  const { data, error, loading, reload } = useApi(ok ? `search:${q}:${JSON.stringify(query)}` : null, (s) =>
    statsSearch({ ...query, q, limit: 20 }, s),
  )
  return (
    <>
      <PageHead title="Search" range />
      <FilterBar />
      <div className="search-box">
        <input
          type="search"
          aria-label="Search prompts and replies"
          placeholder="Search prompts and replies"
          value={text}
          onChange={(e) => setText(e.target.value)}
        />
      </div>
      {!ok ? <Empty>Type at least 2 characters to search</Empty> : null}
      {ok && error ? <ErrorNote error={error} retry={reload} /> : null}
      {ok && loading && !data ? <Empty>Loading…</Empty> : null}
      {ok && data && data.hits.length === 0 ? <Empty>No results. Try other words or a longer range.</Empty> : null}
      {ok && data && data.hits.length > 0 ? (
        <Card>
          <ul className="rows">
            {data.hits.map((h) => (
              <li key={`${h.session_id}:${h.agent_id}:${h.line_id}`}>
                <div className="row-head">
                  <Link className="tl-title" to={`/sessions/${h.session_id}?agent=${h.agent_id}&line=${h.line_id}`}>
                    {h.title}
                  </Link>
                  <span className="sub">
                    {h.role === 'user' ? 'You' : 'Claude'}
                    {h.at ? `, ${fmtDateTime(h.at, me.time_zone)}` : ''}
                  </span>
                </div>
                <p className="row-body">
                  {highlight(h.snippet, q).map((p, i) =>
                    // biome-ignore lint/suspicious/noArrayIndexKey: parts have no identity, order is stable
                    p.hit ? <mark key={i}>{p.text}</mark> : <span key={i}>{p.text}</span>,
                  )}
                </p>
              </li>
            ))}
          </ul>
        </Card>
      ) : null}
    </>
  )
}
