// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useMe } from '../../api/session'
import { statsSessionChat } from '../../api/stats'
import type { ChatLine, SessionDetail } from '../../api/types'
import { buildRows, type Row } from '../../lib/chatRows'
import { fmtDateTime, fmtDuration } from '../../lib/format'
import { assignLanes } from '../../lib/lanes'
import { ErrorNote } from '../ui'
import { ChatEntry } from './ChatEntry'
import { GraphCell } from './GraphCell'

const NEAR_BOTTOM = 80
const nearBottom = () => window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - NEAR_BOTTOM
const stamp = (l: ChatLine) => (l.ts ? Date.parse(l.ts) : Number.NEGATIVE_INFINITY)
const order = (a: ChatLine, b: ChatLine) => stamp(a) - stamp(b) || a.id - b.id
const mergeLines = (a: ChatLine[], b: ChatLine[]) => {
  const seen = new Set(a.map((l) => l.id))
  return [...a, ...b.filter((l) => !seen.has(l.id))].sort(order)
}
const WINDOW = 2000 // a live tick rechecks at most this many of the newest loaded lines
const PAGE = 500

export function SessionGraph(p: { detail: SessionDetail; focusLine?: number }) {
  const { agents, session } = p.detail
  const id = session.session_id
  const { time_zone: tz } = useMe()
  const [lines, setLines] = useState<ChatLine[]>([])
  const [prev, setPrev] = useState<string | null>(null)
  const [next, setNext] = useState<string | null>(null)
  const [error, setError] = useState<unknown>()
  const [loading, setLoading] = useState(true)
  const hhmm = useMemo(() => {
    const f = new Intl.DateTimeFormat('en-GB', { hour: '2-digit', minute: '2-digit', timeZone: tz })
    return (iso: string | null) => (iso ? f.format(new Date(iso)) : '')
  }, [tz])
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())
  const root = useRef<HTMLOListElement>(null)
  const more = useRef<HTMLButtonElement>(null)
  const state = useRef({ lines, next, loading })
  state.current = { lines, next, loading }
  const focused = useRef<number>(undefined)
  const stick = useRef(false)
  const first = useRef(true)
  const epoch = useRef(0) // bumps when the session or deep link changes, so stale page loads are dropped

  useEffect(() => {
    const ctl = new AbortController()
    epoch.current++
    setLines([])
    setPrev(null)
    setNext(null)
    setError(undefined)
    setLoading(true)
    focused.current = undefined
    statsSessionChat(id, p.focusLine ? { at: p.focusLine } : {}, ctl.signal).then(
      (pg) => {
        if (ctl.signal.aborted) return
        setLines(pg.lines ?? [])
        setPrev(pg.prev ?? null)
        setNext(pg.next ?? null)
        setLoading(false)
      },
      (e: unknown) => {
        if (ctl.signal.aborted) return
        setError(e)
        setLoading(false)
      },
    )
    return () => ctl.abort()
  }, [id, p.focusLine])

  // Live: the session detail reloads on session_updated; once the end is loaded, refetch the newest loaded window
  // forward, so late lines (a subagent synced after its parent) land in their time position.
  // shortcut: a line later than WINDOW lines behind the end shows only after a reload of the view.
  // biome-ignore lint/correctness/useExhaustiveDependencies: the detail object is the trigger, the rest is read from the ref
  useEffect(() => {
    if (first.current) {
      first.current = false
      return
    }
    const { lines: cur, next: n, loading: busy } = state.current
    if (n !== null || busy || cur.length === 0) return
    const ctl = new AbortController()
    void (async () => {
      let q: { at?: number; after?: string } = { at: cur[Math.max(0, cur.length - WINDOW)]?.id }
      const got: ChatLine[] = []
      let nxt: string | null = null
      for (let i = 0; i < WINDOW / PAGE + 2; i++) {
        const pg = await statsSessionChat(id, { ...q, limit: PAGE }, ctl.signal)
        got.push(...(pg.lines ?? []))
        nxt = pg.next ?? null
        if (nxt === null) break
        q = { after: nxt }
      }
      if (ctl.signal.aborted) return
      stick.current = nearBottom()
      setLines((old) => mergeLines(old, got))
      setNext(nxt)
    })().catch(() => {})
    return () => ctl.abort()
  }, [p.detail])

  // biome-ignore lint/correctness/useExhaustiveDependencies: new lines are the trigger
  useLayoutEffect(() => {
    if (stick.current) window.scrollTo(0, document.documentElement.scrollHeight)
    stick.current = false
  }, [lines])

  const load = (
    q: { after?: string; before?: string },
    apply: (pg: Awaited<ReturnType<typeof statsSessionChat>>) => void,
  ) => {
    const mine = epoch.current
    setLoading(true)
    setError(undefined)
    statsSessionChat(id, q).then(
      (pg) => {
        if (mine !== epoch.current) return
        apply(pg)
        setLoading(false)
      },
      (e: unknown) => {
        if (mine !== epoch.current) return
        setError(e)
        setLoading(false)
      },
    )
  }
  const later = () =>
    next !== null &&
    load({ after: next }, (pg) => {
      setLines((old) => mergeLines(old, pg.lines ?? []))
      setNext(pg.next ?? null)
    })
  const earlier = () =>
    prev !== null &&
    load({ before: prev }, (pg) => {
      setLines((old) => mergeLines(old, pg.lines ?? []))
      setPrev(pg.prev ?? null)
    })

  // The button is also the sentinel: it loads the next page as soon as it scrolls into view. A page may hold few
  // visible entries, so each new page re-observes and keeps loading while the sentinel stays in view.
  // biome-ignore lint/correctness/useExhaustiveDependencies: `later` is rebuilt every render; the deps below cover what it reads
  useEffect(() => {
    const el = more.current
    if (!el || next === null || loading || error !== undefined || typeof IntersectionObserver === 'undefined') return
    const io = new IntersectionObserver((es) => es.some((e) => e.isIntersecting) && later())
    io.observe(el)
    return () => io.disconnect()
  }, [next, loading, error, lines.length])

  const byId = useMemo(() => new Map(agents.map((a) => [a.agent_id, a])), [agents])
  const lanes = useMemo(
    () =>
      assignLanes(
        agents.map((a) => ({
          id: a.agent_id,
          parent: a.parent_agent_id,
          from: a.spawned_at ?? a.started_at,
          to: a.merged_at,
        })),
      ),
    [agents],
  )
  const rows = useMemo(() => buildRows(lines, agents, lanes, collapsed), [lines, agents, lanes, collapsed])
  const lineTs = useMemo(() => new Map(lines.map((l) => [l.id, l.ts])), [lines])
  const subs = useMemo(() => agents.filter((a) => a.agent_id !== 'main').map((a) => a.agent_id), [agents])

  // A deep link opens the branch it points into (and the branches above it).
  useEffect(() => {
    const l = lines.find((x) => x.id === p.focusLine)
    if (!l) return
    setCollapsed((old) => {
      const n = new Set(old)
      for (let a = byId.get(l.agent_id); a; a = a.parent_agent_id ? byId.get(a.parent_agent_id) : undefined)
        n.delete(a.agent_id)
      return n.size === old.size ? old : n
    })
  }, [lines, p.focusLine, byId])
  // biome-ignore lint/correctness/useExhaustiveDependencies: run when rows arrive; `focused` stops repeat scrolling
  useEffect(() => {
    if (!p.focusLine || focused.current === p.focusLine) return
    const el = root.current?.querySelector<HTMLElement>('li[aria-current="true"]')
    if (!el) return
    focused.current = p.focusLine
    el.scrollIntoView?.({ block: 'center' })
    el.focus()
  }, [rows, p.focusLine])

  // Which lanes run through each row, including branches opened above the loaded window.
  const cells = useMemo(() => {
    const lastEntry = new Map<string, number>()
    rows.forEach((r, i) => {
      if (r.kind === 'entry') lastEntry.set(r.agentId, i)
    })
    const ends = new Map<number, string[]>() // an open branch ends at its last entry row
    rows.forEach((r, i) => {
      if (r.kind !== 'branch' || !r.open) return
      const at = Math.max(i, lastEntry.get(r.agentId) ?? i)
      ends.set(at, [...(ends.get(at) ?? []), r.agentId])
    })
    const running = new Map<string, number>()
    const t0 = prev !== null && lines[0]?.ts ? Date.parse(lines[0].ts) : Number.NaN
    for (const a of agents) {
      if (a.agent_id === 'main' || collapsed.has(a.agent_id) || !a.spawned_at || !(Date.parse(a.spawned_at) < t0))
        continue
      if (a.merged_at && !(Date.parse(a.merged_at) > t0)) continue
      running.set(a.agent_id, lanes.get(a.agent_id) ?? 0)
      const end = lastEntry.get(a.agent_id)
      if (!a.merged_at && end !== undefined) ends.set(end, [...(ends.get(end) ?? []), a.agent_id])
    }
    let lastDot = -1
    rows.forEach((r, i) => {
      if (r.kind === 'entry') lastDot = i
    })
    return rows.map((r, i) => {
      const through = [...running].filter(([a]) => r.kind !== 'merge' || a !== r.agentId).map(([, l]) => l)
      if (r.kind === 'branch') running.set(r.agentId, r.lane)
      if (r.kind === 'merge') running.delete(r.agentId)
      for (const a of ends.get(i) ?? []) running.delete(a)
      return {
        through,
        last: i === rows.length - 1 && running.size === 0,
        pulse: session.is_live && next === null && i === lastDot,
      }
    })
  }, [rows, session.is_live, next, prev, lines, agents, lanes, collapsed])

  const allCollapsed = subs.length > 0 && subs.every((a) => collapsed.has(a))
  const toggle = (a: string) =>
    setCollapsed((old) => {
      const n = new Set(old)
      if (!n.delete(a)) n.add(a)
      return n
    })
  const render = (r: Row) => {
    if (r.kind === 'entry') {
      const a = byId.get(r.agentId)
      return <ChatEntry e={r.entry} tag={r.agentId === 'main' ? undefined : (a?.agent_type ?? r.agentId)} />
    }
    const a = byId.get(r.agentId)
    if (r.kind === 'branch')
      return (
        <div className="g-branch">
          <button
            type="button"
            className="btn ghost"
            aria-expanded={!collapsed.has(r.agentId)}
            aria-label={`Subagent ${a?.agent_type ?? r.agentId}: ${a?.description ?? ''}`}
            onClick={() => toggle(r.agentId)}
          >
            {collapsed.has(r.agentId) ? '▸' : '▾'}
          </button>
          <span className="gtag">{a?.agent_type ?? r.agentId}</span> {r.label}
          {r.open ? <span className="g-peek"> still open</span> : null}
        </div>
      )
    const took = r.at && a?.spawned_at ? Date.parse(r.at) - Date.parse(a.spawned_at) : Number.NaN
    return (
      <div className="g-merge">
        ↩ <span className="gtag">{a?.agent_type ?? r.agentId}</span> back
        {Number.isFinite(took) && took >= 0 ? ` after ${fmtDuration(took)}` : ''}: {r.summary.split('\n')[0]}
      </div>
    )
  }

  return (
    <section className="card graph" aria-label="Session chat">
      <div className="list-head">
        <h2>Chat</h2>
        {subs.length > 0 ? (
          <button
            type="button"
            className="btn ghost"
            onClick={() => setCollapsed(allCollapsed ? new Set() : new Set(subs))}
          >
            {allCollapsed ? 'Expand subagents' : 'Collapse subagents'}
          </button>
        ) : null}
      </div>
      {error ? <ErrorNote error={error} /> : null}
      {prev !== null ? (
        <button type="button" className="btn" disabled={loading} onClick={earlier}>
          Load earlier
        </button>
      ) : null}
      <ol className="grows" ref={root}>
        {rows.map((r, i) => {
          const c = cells[i]
          const at = r.kind === 'entry' ? (r.entry.at ?? lineTs.get(r.entry.lineId) ?? null) : r.at
          const focus = r.kind === 'entry' && r.entry.lineId === p.focusLine
          return (
            <li key={r.key} className={`grow ${r.kind}`} tabIndex={-1} aria-current={focus ? 'true' : undefined}>
              <time dateTime={at ?? undefined} title={at ? fmtDateTime(at, tz) : undefined}>
                {hhmm(at)}
              </time>
              <GraphCell row={r} through={c?.through ?? []} last={c?.last ?? false} pulse={c?.pulse ?? false} />
              <div className="gbody">{render(r)}</div>
            </li>
          )
        })}
      </ol>
      {loading && rows.length === 0 ? <p>Loading…</p> : null}
      {next !== null ? (
        <button type="button" className="btn" ref={more} disabled={loading} onClick={later}>
          Load more
        </button>
      ) : null}
    </section>
  )
}
