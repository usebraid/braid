// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { useEffect, useMemo, useRef, useState } from 'react'
import { statsSessionLines } from '../api/stats'
import type { LineItem } from '../api/types'
import { type Entry, entriesOf } from '../lib/transcript'
import { useAgentParam } from '../lib/useAgentParam'
import { ErrorNote, Pill } from './ui'

const LIMIT = 100
// A malformed page (missing fields) reads as an empty, finished one instead of crashing the panel.
const fetchLines = async (session: string, agent: string, q: Record<string, number>, signal?: AbortSignal) => {
  const pg = await statsSessionLines(session, { agent, limit: LIMIT, ...q }, signal)
  return { lines: pg.lines ?? [], prev: pg.prev ?? null, next: pg.next ?? null }
}
const merge = (a: LineItem[], b: LineItem[]) => {
  const seen = new Set(a.map((l) => l.id))
  return [...a, ...b.filter((l) => !seen.has(l.id))]
}
const LABEL: Record<Entry['kind'], string> = {
  prompt: 'Prompt',
  reply: 'Reply',
  thinking: 'Thinking',
  tool_call: 'Tool call',
  tool_result: 'Tool result',
  system: 'System',
  event: 'Event',
}

function Body({ e }: { e: Entry }) {
  const text = (
    <pre className="entry-text">
      {e.text}
      {e.truncated ? '\n… truncated' : ''}
    </pre>
  )
  if (e.kind !== 'tool_call' && e.kind !== 'tool_result') return text
  return (
    <details>
      <summary>
        {e.tool?.name ? `${e.tool.name} ` : ''}
        {e.text.slice(0, 80)}
      </summary>
      {text}
    </details>
  )
}

// Everything from the transcript is rendered as text, never as markup.
export function TranscriptPanel(p: { sessionId: string; agentId: string; focusLine?: number; liveTick: unknown }) {
  const [, select] = useAgentParam()
  const [items, setItems] = useState<LineItem[]>([])
  const [prev, setPrev] = useState<string | null>(null)
  const [next, setNext] = useState<string | null>(null)
  const [error, setError] = useState<unknown>()
  const [loading, setLoading] = useState(true)
  const root = useRef<HTMLElement>(null)
  const state = useRef({ items, next, loading })
  state.current = { items, next, loading }
  const focused = useRef<number>(undefined)
  const firstTick = useRef(true)

  useEffect(() => {
    const ctl = new AbortController()
    setItems([])
    setPrev(null)
    setNext(null)
    setError(undefined)
    setLoading(true)
    focused.current = undefined
    fetchLines(p.sessionId, p.agentId, { after: p.focusLine ? p.focusLine - 1 : 0 }, ctl.signal).then(
      (pg) => {
        if (ctl.signal.aborted) return
        setItems(pg.lines)
        setPrev(pg.prev)
        setNext(pg.next)
        setLoading(false)
      },
      (e: unknown) => {
        if (ctl.signal.aborted) return
        setError(e)
        setLoading(false)
      },
    )
    return () => ctl.abort()
  }, [p.sessionId, p.agentId, p.focusLine])

  // Live: when the end was loaded, append what is new after the last line, in place.
  // biome-ignore lint/correctness/useExhaustiveDependencies: liveTick is the trigger, the rest is read from the ref
  useEffect(() => {
    if (firstTick.current) {
      firstTick.current = false
      return
    }
    const { items: cur, next: n, loading: busy } = state.current
    if (n !== null || busy) return
    const ctl = new AbortController()
    fetchLines(p.sessionId, p.agentId, { after: cur.at(-1)?.id ?? 0 }, ctl.signal).then(
      (pg) => {
        if (ctl.signal.aborted) return
        setItems((old) => merge(old, pg.lines))
        setNext(pg.next)
      },
      () => {},
    )
    return () => ctl.abort()
  }, [p.liveTick])

  const more = (q: Record<string, number>, apply: (pg: Awaited<ReturnType<typeof fetchLines>>) => void) => {
    setLoading(true)
    fetchLines(p.sessionId, p.agentId, q).then(
      (pg) => {
        apply(pg)
        setLoading(false)
      },
      (e: unknown) => {
        setError(e)
        setLoading(false)
      },
    )
  }
  const earlier = () =>
    more({ before: items[0]?.id ?? 0 }, (pg) => {
      setItems((old) => merge(pg.lines, old))
      setPrev(pg.prev)
    })
  const later = () =>
    more({ after: Number(next) }, (pg) => {
      setItems((old) => merge(old, pg.lines))
      setNext(pg.next)
    })

  const entries = useMemo(() => items.flatMap(entriesOf), [items])
  // biome-ignore lint/correctness/useExhaustiveDependencies: run when entries arrive; `focused` stops repeat scrolling
  useEffect(() => {
    if (!p.focusLine || focused.current === p.focusLine) return
    const el = root.current?.querySelector<HTMLElement>('article[aria-current="true"]')
    if (!el) return
    focused.current = p.focusLine
    el.scrollIntoView?.({ block: 'center' })
    el.focus()
  }, [entries, p.focusLine])

  return (
    <aside className="card transcript" aria-label="Transcript" ref={root}>
      <div className="list-head">
        <h2>{p.agentId}</h2>
        <button type="button" className="btn" onClick={() => select(null)}>
          Close
        </button>
      </div>
      {error ? <ErrorNote error={error} /> : null}
      {prev !== null ? (
        <button type="button" className="btn" disabled={loading} onClick={earlier}>
          Load earlier
        </button>
      ) : null}
      {entries.map((e) => (
        <article
          key={e.key}
          className={`entry ${e.kind}`}
          tabIndex={-1}
          aria-current={e.lineId === p.focusLine ? 'true' : undefined}
        >
          <header>
            <b>{LABEL[e.kind]}</b>
            {e.at ? <time dateTime={e.at}>{e.at}</time> : null}
            {e.isError ? <Pill kind="bad">error</Pill> : null}
          </header>
          <Body e={e} />
        </article>
      ))}
      {loading && entries.length === 0 ? <p>Loading…</p> : null}
      {next !== null ? (
        <button type="button" className="btn" disabled={loading} onClick={later}>
          Load more
        </button>
      ) : null}
    </aside>
  )
}
