// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { LiveClient, type LiveEvent } from './live'

class FakeSocket {
  static all: FakeSocket[] = []
  onopen: (() => void) | null = null
  onmessage: ((e: { data: string }) => void) | null = null
  onclose: ((e: { code: number }) => void) | null = null
  closed = false
  constructor(readonly url: string) {
    FakeSocket.all.push(this)
  }
  close() {
    this.closed = true
    this.onclose?.({ code: 1000 })
  }
  open() {
    this.onopen?.()
  }
  send(o: unknown) {
    this.onmessage?.({ data: typeof o === 'string' ? o : JSON.stringify(o) })
  }
  drop(code = 1006) {
    this.onclose?.({ code })
  }
}
const make = (extra = {}) =>
  new LiveClient({
    url: 'ws://x/api/v1/live',
    WebSocketImpl: FakeSocket as unknown as typeof WebSocket,
    random: () => 0.5,
    ...extra,
  })
const last = () => FakeSocket.all[FakeSocket.all.length - 1] as FakeSocket

beforeEach(() => {
  FakeSocket.all = []
  vi.useFakeTimers()
})
afterEach(() => vi.useRealTimers())

describe('LiveClient', () => {
  it('connects, reports status and delivers events but not ping/hello/garbage', () => {
    const c = make()
    const got: LiveEvent[] = []
    c.subscribe((e) => got.push(e))
    c.start()
    expect(c.status).toBe('connecting')
    last().open()
    last().send({ type: 'hello', protocol: 1 })
    expect(c.status).toBe('open')
    last().send({ type: 'ping' })
    last().send('not json')
    last().send({ type: 'mystery' })
    last().send({ type: 'session_updated', session_id: 'g1' })
    expect(got).toEqual([{ type: 'session_updated', session_id: 'g1' }])
  })
  it('backs off 1s, 2s, 4s, caps at 30s, and resets after hello', () => {
    const c = make()
    c.start()
    const delays: number[] = []
    for (let i = 0; i < 7; i++) {
      const n = FakeSocket.all.length
      last().drop()
      expect(c.status).toBe('paused')
      let waited = 0
      while (FakeSocket.all.length === n) {
        vi.advanceTimersByTime(500)
        waited += 500
      }
      delays.push(waited)
    }
    expect(delays).toEqual([1000, 2000, 4000, 8000, 16000, 30000, 30000])
    last().open()
    last().send({ type: 'hello', protocol: 1 })
    const n = FakeSocket.all.length
    last().drop()
    vi.advanceTimersByTime(1000)
    expect(FakeSocket.all.length).toBe(n + 1)
  })
  it('emits one resync after a reconnect but not on the first connect', () => {
    const c = make()
    const got: string[] = []
    c.subscribe((e) => got.push(e.type))
    c.start()
    last().open()
    last().send({ type: 'hello', protocol: 1 })
    expect(got).toEqual([])
    last().drop()
    vi.advanceTimersByTime(1000)
    last().open()
    last().send({ type: 'hello', protocol: 1 })
    expect(got).toEqual(['resync'])
  })
  it('does not reconnect after 4401 and calls onAuthLost', () => {
    const lost = vi.fn()
    const c = make({ onAuthLost: lost })
    c.start()
    last().open()
    last().drop(4401)
    vi.advanceTimersByTime(60_000)
    expect(lost).toHaveBeenCalledOnce()
    expect(FakeSocket.all).toHaveLength(1)
  })
  it('reconnects when the server goes silent for 70 s', () => {
    const c = make()
    c.start()
    last().open()
    last().send({ type: 'hello', protocol: 1 })
    vi.advanceTimersByTime(70_000)
    expect(FakeSocket.all[0]?.closed).toBe(true)
    vi.advanceTimersByTime(1500)
    expect(FakeSocket.all.length).toBe(2)
  })
  it('stop() closes and never reconnects', () => {
    const c = make()
    c.start()
    last().open()
    c.stop()
    vi.advanceTimersByTime(120_000)
    expect(FakeSocket.all).toHaveLength(1)
    expect(FakeSocket.all[0]?.closed).toBe(true)
  })
})
