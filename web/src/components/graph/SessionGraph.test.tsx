// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { act, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { LiveSource } from '../../api/live'
import type { AgentItem, ChatLine, SessionDetail } from '../../api/types'
import type { LiveEvent } from '../../lib/live'
import { renderApp } from '../../test/render'
import { Session } from '../../views/Session'
import { SessionGraph } from './SessionGraph'

const usage = { cost_usd: 0, tokens: 0, input: 0, output: 0, cache_read: 0, cache_write: 0, messages: 0 }
const ag = (id: string, o: Partial<AgentItem> = {}): AgentItem => ({
  agent_id: id,
  agent_type: id === 'main' ? 'main' : 'Explore',
  description: `d-${id}`,
  parent_agent_id: id === 'main' ? null : 'main',
  spawn_tool_use_id: null,
  spawn_depth: 0,
  started_at: null,
  ended_at: null,
  spawned_at: null,
  merged_at: null,
  finished: null,
  duration_ms: 0,
  duration_estimated: false,
  tool_calls: {},
  tool_errors: 0,
  usage,
  ...o,
})
const detail = (live = false): SessionDetail =>
  ({
    session: {
      session_id: 'g1',
      title: 'T',
      is_live: live,
      device_ids: [],
      models: [],
      project_name: 'p',
      agent_ms: 0,
      cost_usd: 0,
    },
    agents: [
      ag('main'),
      ag('a1', { spawn_tool_use_id: 't1', spawned_at: '2026-11-01T10:02:00Z', merged_at: '2026-11-01T10:04:10Z' }),
    ],
    skill_phases: [],
    failures: [],
  }) as unknown as SessionDetail

const L = (id: number, agent: string, minute: number, line: unknown): ChatLine => ({
  id,
  agent_id: agent,
  ts: `2026-11-01T10:0${minute}:00Z`,
  offset: id,
  line: line as ChatLine['line'],
})
const asst = (content: unknown) => ({ type: 'assistant', message: { content } })
const LINES = [
  L(1, 'main', 0, { type: 'user', message: { content: 'build the export' } }),
  L(2, 'main', 2, asst([{ type: 'tool_use', id: 't1', name: 'Task', input: { description: 'find pdf libs' } }])),
  L(3, 'a1', 3, asst([{ type: 'text', text: 'grepping inside the subagent' }])),
  L(4, 'main', 4, {
    type: 'user',
    message: { content: [{ type: 'tool_result', tool_use_id: 't1', content: 'pdfkit is already there\nmore' }] },
  }),
  L(5, 'main', 5, asst('all done')),
]
const get = vi.fn(async (_p: string, q?: Record<string, string | number>): Promise<unknown> => {
  void q
  return { lines: LINES, next: null, prev: null }
})
vi.mock('../../api/client', async (orig) => ({
  ...(await orig<typeof import('../../api/client')>()),
  get: (...a: unknown[]) => get(...(a as [string, Record<string, string | number>])),
}))

beforeEach(() => {
  get.mockClear()
  window.scrollTo = vi.fn()
})
afterEach(() => {
  Object.defineProperty(document.documentElement, 'scrollHeight', { configurable: true, value: 0 })
})

it('renders prompt, tool call, branch, merge with duration and result summary, and collapses a subagent', async () => {
  renderApp(<SessionGraph detail={detail()} />)
  expect(await screen.findByText('build the export')).toBeInTheDocument()
  expect(screen.getByText('grepping inside the subagent')).toBeInTheDocument()
  expect(document.querySelector('.g-merge')).toHaveTextContent('Explore back after 2m 0s: pdfkit is already there')
  expect(document.querySelectorAll('svg[aria-hidden="true"]').length).toBe(5)
  const toggle = screen.getByRole('button', { name: /Subagent Explore: d-a1/ })
  expect(toggle).toHaveAttribute('aria-expanded', 'true')
  await userEvent.click(toggle)
  expect(toggle).toHaveAttribute('aria-expanded', 'false')
  expect(screen.queryByText('grepping inside the subagent')).toBeNull()
  expect(screen.getAllByText(/1 step/)).toHaveLength(2) // branch label and merge summary
  await userEvent.click(toggle)
  expect(screen.getByText('grepping inside the subagent')).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Collapse subagents' }))
  expect(screen.queryByText('grepping inside the subagent')).toBeNull()
  expect(screen.getByRole('button', { name: 'Expand subagents' })).toBeInTheDocument()
})

it('marks the focused row', async () => {
  renderApp(<SessionGraph detail={detail()} focusLine={3} />)
  const row = (await screen.findByText('grepping inside the subagent')).closest('li')
  await waitFor(() => expect(row).toHaveAttribute('aria-current', 'true'))
  expect(row).toHaveFocus()
  expect(get.mock.calls[0]?.[1]).toMatchObject({ at: 3 })
})

it('marks the nearest row at or after a focused line without a row, else the one before', async () => {
  const empty = (id: number, minute: number) => L(id, 'main', minute, asst([]))
  get.mockResolvedValueOnce({ lines: [...LINES, empty(6, 4)], next: null, prev: null })
  const first = renderApp(<SessionGraph detail={detail()} focusLine={6} />)
  const after = (await screen.findByText('all done')).closest('li')
  await waitFor(() => expect(after).toHaveAttribute('aria-current', 'true'))
  expect(after).toHaveFocus()
  first.unmount()
  get.mockResolvedValueOnce({ lines: [...LINES, empty(7, 5)], next: null, prev: null })
  renderApp(<SessionGraph detail={detail()} focusLine={7} />)
  const before = (await screen.findByText('all done')).closest('li')
  await waitFor(() => expect(before).toHaveAttribute('aria-current', 'true'))
})

it('pages forward through the next cursor', async () => {
  get.mockResolvedValueOnce({ lines: LINES.slice(0, 1), next: 'c1', prev: null })
  get.mockResolvedValueOnce({ lines: LINES.slice(4), next: null, prev: null })
  renderApp(<SessionGraph detail={detail()} />)
  await userEvent.click(await screen.findByRole('button', { name: 'Load more' }))
  expect(await screen.findByText('all done')).toBeInTheDocument()
  expect(get.mock.calls[1]?.[1]).toMatchObject({ after: 'c1' })
  expect(screen.queryByRole('button', { name: 'Load more' })).toBeNull()
})

function liveHarness() {
  const subs = new Set<(e: LiveEvent) => void>()
  const live: LiveSource = {
    status: 'open',
    start() {},
    stop() {},
    subscribe: (f) => {
      subs.add(f)
      return () => subs.delete(f)
    },
    onStatus: () => () => {},
  }
  return {
    live,
    emit: (e: LiveEvent) =>
      act(() => {
        for (const f of subs) f(e)
      }),
  }
}
const liveDetail = { ...detail(true), agents: detail().agents }

async function liveCase(scrolledUp: boolean) {
  const { live, emit } = liveHarness()
  get.mockImplementation(async (p: string, q?: Record<string, string | number>) => {
    if (p.endsWith('/chat'))
      return q?.at === 1 // a late line: newer id, but a time before the last loaded line
        ? { lines: [...LINES, L(6, 'a1', 1, asst('fresh line'))], next: null, prev: null }
        : { lines: LINES, next: null, prev: null }
    return p.endsWith('/devices') ? [] : { ...liveDetail } // a reload yields a new object, as parsed JSON does
  })
  renderApp(
    <Routes>
      <Route path="/sessions/:id" element={<Session />} />
    </Routes>,
    { route: '/sessions/g1', live },
  )
  await screen.findByText('all done')
  if (scrolledUp) Object.defineProperty(document.documentElement, 'scrollHeight', { configurable: true, value: 9000 })
  emit({ type: 'session_updated', session_id: 'g1' })
  expect(await screen.findByText('fresh line')).toBeInTheDocument()
  expect(get.mock.calls.some(([, q]) => q?.at === 1)).toBe(true)
  const texts = within(screen.getByLabelText('Session chat'))
    .getAllByRole('listitem')
    .map((li) => li.textContent ?? '')
  const at = (t: string) => texts.findIndex((x) => x.includes(t))
  expect(at('fresh line')).toBeGreaterThan(at('build the export'))
  expect(at('fresh line')).toBeLessThan(at('d-a1'))
  expect(within(screen.getByLabelText('Session chat')).getAllByText('all done')).toHaveLength(1)
}
it('appends a live line and keeps the bottom in view when the reader is at the end', async () => {
  await liveCase(false)
  expect(window.scrollTo).toHaveBeenCalled()
})
it('appends a live line without moving the scroll position when the reader scrolled up', async () => {
  await liveCase(true)
  expect(window.scrollTo).not.toHaveBeenCalled()
})

function Host() {
  const [line, setLine] = useState<number>()
  return (
    <>
      <button type="button" onClick={() => setLine(3)}>
        go
      </button>
      <SessionGraph detail={detail()} focusLine={line} />
    </>
  )
}

it('expands a collapsed branch when a deep link points into it', async () => {
  renderApp(<Host />)
  await userEvent.click(await screen.findByRole('button', { name: 'Collapse subagents' }))
  expect(screen.queryByText('grepping inside the subagent')).toBeNull()
  await userEvent.click(screen.getByRole('button', { name: 'go' }))
  expect(await screen.findByText('grepping inside the subagent')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: /Subagent Explore/ })).toHaveAttribute('aria-expanded', 'true')
})

it('stops auto-loading after an error and keeps the button as a manual retry', async () => {
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      constructor(private cb: (e: { isIntersecting: boolean }[]) => void) {}
      observe() {
        this.cb([{ isIntersecting: true }])
      }
      disconnect() {}
    },
  )
  try {
    get.mockResolvedValueOnce({ lines: LINES.slice(0, 1), next: 'c1', prev: null })
    get.mockRejectedValue(new Error('boom'))
    renderApp(<SessionGraph detail={detail()} />)
    await screen.findByRole('alert')
    await new Promise((r) => setTimeout(r, 200))
    expect(get).toHaveBeenCalledTimes(2) // first page plus one failed attempt
    expect(screen.getByRole('button', { name: 'Load more' })).toBeEnabled()
  } finally {
    vi.unstubAllGlobals()
    get.mockReset()
    get.mockResolvedValue({ lines: LINES, next: null, prev: null })
  }
})
