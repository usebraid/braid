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
const TAIL = 200 // a live tick that expects late subagent lines refetches this many of the newest loaded lines
const PAGE = 500
const MAX_PAGES = 10 // per tick; the "Load more" sentinel takes over beyond

// The `after` cursor of a loaded line (api/openapi.yaml, statsSessionChat).
const cursorOf = (l: ChatLine) =>
  btoa(`${l.ts ?? '-infinity'}|${l.id}`)
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '')

// An agent whose last activity lies inside the loaded window but after its newest loaded line has late lines there.
const hasLateLines = (agents: SessionDetail['agents'], lines: ChatLine[]) => {
  const newest = new Map<string, number>()
  for (const l of lines) newest.set(l.agent_id, Math.max(newest.get(l.agent_id) ?? Number.NEGATIVE_INFINITY, stamp(l)))
  const first = lines.find((l) => l.ts)
  const lo = first ? stamp(first) : Number.POSITIVE_INFINITY
  const hi = stamp(lines[lines.length - 1] as ChatLine)
  return agents.some((a) => {
    const end = a.ended_at ? Date.parse(a.ended_at) : Number.NaN
    return end >= lo && end <= hi && end > (newest.get(a.agent_id) ?? Number.NEGATIVE_INFINITY)
  })
}

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
  const live = useRef(session.is_live)
  live.current = session.is_live

  useEffect(() => {
    const ctl = new AbortController()
    epoch.current++
    setLines([])
    setPrev(null)
    setNext(null)
    setError(undefined)
    setLoading(true)
    focused.current = undefined
    // a live session opens at its newest page with the bottom in view; a finished one at its start
    const latest = !p.focusLine && live.current
    statsSessionChat(id, p.focusLine ? { at: p.focusLine } : latest ? { latest: 1 } : {}, ctl.signal).then(
      (pg) => {
        if (ctl.signal.aborted) return
        stick.current = latest
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

  // Live: the session detail reloads on session_updated; once the end is loaded, fetch the lines after it. Only when an
  // agent has activity inside the loaded window that is not loaded (a subagent synced after its parent), refetch the
  // newest TAIL lines too, so those late lines land in their time position.
  // shortcut: a late line more than TAIL lines behind the end shows only after a reload of the view.
  // biome-ignore lint/correctness/useExhaustiveDependencies: the detail object is the trigger, the rest is read from the ref
  useEffect(() => {
    if (first.current) {
      first.current = false
      return
    }
    const { lines: cur, next: n, loading: busy } = state.current
    if (n !== null || busy || cur.length === 0) return
    const ctl = new AbortController()
    const end = cur[cur.length - 1] as ChatLine
    void (async () => {
      let q: { at?: number; after?: string } = hasLateLines(p.detail.agents, cur)
        ? { at: cur[Math.max(0, cur.length - TAIL)]?.id }
        : { after: cursorOf(end) }
      const got: ChatLine[] = []
      let nxt: string | null = null
      for (let i = 0; i < MAX_PAGES; i++) {
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
  const atOf = (r: Row) => (r.kind === 'entry' ? (r.entry.at ?? lineTs.get(r.entry.lineId) ?? null) : r.at)
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

  // A deep link to a line without a row (an empty API-error reply, a collapsed branch) marks the nearest row at or after
  // it in line order, else the nearest before.
  const focusKey = useMemo(() => {
    if (!p.focusLine) return undefined
    const at = new Map(lines.map((l, i) => [l.id, i]))
    const f = at.get(p.focusLine)
    if (f === undefined) return undefined
    let after: [number, string] | undefined
    let before: [number, string] | undefined
    for (const r of rows) {
      const i = r.kind === 'entry' ? at.get(r.entry.lineId) : undefined
      if (i === undefined) continue
      if (i >= f ? !after || i < after[0] : !before || i >= before[0]) {
        if (i >= f) after = [i, r.key]
        else before = [i, r.key]
      }
    }
    return (after ?? before)?.[1]
  }, [rows, lines, p.focusLine])

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
      // an open agent from above ends at its last loaded row, or at once when none is loaded: never a lane to the bottom
      const end = lastEntry.get(a.agent_id) ?? 0
      if (!a.merged_at) ends.set(end, [...(ends.get(end) ?? []), a.agent_id])
    }
    let lastDot = -1
    rows.forEach((r, i) => {
      if (r.kind === 'entry') lastDot = i
    })
    return rows.map((r, i) => {
      const through = [...running].filter(([a]) => r.kind !== 'merge' || a !== r.agentId).map(([, l]) => l)
      if (r.kind === 'branch') running.set(r.agentId, r.lane)
      if (r.kind === 'merge') running.delete(r.agentId)
      const ended = (ends.get(i) ?? []).map((a) => running.get(a) ?? r.lane)
      for (const a of ends.get(i) ?? []) running.delete(a)
      const still = new Set(running.values())
      return {
        through,
        stubs: ended.filter((l) => !still.has(l)),
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
      return (
        <ChatEntry e={r.entry} lane={r.lane} tag={r.agentId === 'main' ? undefined : (a?.agent_type ?? r.agentId)} />
      )
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
          const at = atOf(r)
          const prevAt = i > 0 ? atOf(rows[i - 1] as Row) : null
          // the time only where it changes or a prompt starts a turn, so the column stays quiet
          const showTime = hhmm(at) !== hhmm(prevAt) || (r.kind === 'entry' && r.entry.kind === 'prompt')
          const focus = r.key === focusKey
          return (
            <li
              key={r.key}
              className={`grow grow--${r.kind === 'entry' ? r.entry.kind : r.kind}${c?.stubs.length ? ' grow--ends' : ''}`}
              tabIndex={-1}
              aria-current={focus ? 'true' : undefined}
            >
              <time dateTime={at ?? undefined} title={at ? fmtDateTime(at, tz) : undefined}>
                {showTime ? hhmm(at) : ''}
              </time>
              <GraphCell
                row={r}
                through={c?.through ?? []}
                stubs={c?.stubs ?? []}
                last={c?.last ?? false}
                pulse={c?.pulse ?? false}
              />
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
