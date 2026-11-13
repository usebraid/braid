// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

export type LiveEvent =
  | { type: 'session_updated'; session_id: string }
  | { type: 'device_synced'; device_id: string }
  | { type: 'reindex'; state: 'started' | 'done' }
  | { type: 'resync' }
export type LiveStatus = 'connecting' | 'open' | 'paused'
export type LiveOptions = {
  url: string
  WebSocketImpl?: typeof WebSocket
  random?: () => number
  onAuthLost?: () => void
}

const SILENCE_MS = 70_000

export class LiveClient {
  status: LiveStatus = 'connecting'
  private ws: WebSocket | null = null
  private attempt = 0
  private retry: ReturnType<typeof setTimeout> | undefined
  private watchdog: ReturnType<typeof setTimeout> | undefined
  private running = false
  private resync = false
  private readonly events = new Set<(e: LiveEvent) => void>()
  private readonly statuses = new Set<(s: LiveStatus) => void>()

  constructor(private readonly o: LiveOptions) {}

  start(): void {
    if (this.running) return
    this.running = true
    this.connect()
  }

  stop(): void {
    this.running = false
    clearTimeout(this.retry)
    this.detach()
  }

  subscribe(fn: (e: LiveEvent) => void): () => void {
    this.events.add(fn)
    return () => this.events.delete(fn)
  }

  onStatus(fn: (s: LiveStatus) => void): () => void {
    this.statuses.add(fn)
    return () => this.statuses.delete(fn)
  }

  private set(s: LiveStatus) {
    if (s === this.status) return
    this.status = s
    for (const f of this.statuses) f(s)
  }

  private detach() {
    clearTimeout(this.watchdog)
    const ws = this.ws
    this.ws = null
    if (!ws) return
    ws.onopen = ws.onmessage = ws.onclose = null
    ws.close()
  }

  private arm() {
    clearTimeout(this.watchdog)
    this.watchdog = setTimeout(() => this.lost(), SILENCE_MS)
  }

  private connect() {
    const Impl = this.o.WebSocketImpl ?? WebSocket
    const ws = new Impl(this.o.url)
    this.ws = ws
    ws.onopen = () => this.arm()
    ws.onmessage = (m) => this.frame(m.data)
    ws.onclose = (e) => {
      this.ws = null
      this.lost(e.code)
    }
  }

  // The socket is gone (or presumed dead): pause and schedule a reconnect, unless auth is lost.
  private lost(code?: number) {
    if (!this.running) return
    this.detach()
    if (code === 4401) {
      this.running = false
      this.set('paused')
      this.o.onAuthLost?.()
      return
    }
    this.set('paused')
    this.resync = true
    const rnd = (this.o.random ?? Math.random)()
    const delay = Math.min(30_000, 1000 * 2 ** this.attempt) * (0.8 + 0.4 * rnd)
    this.attempt++
    this.retry = setTimeout(() => this.connect(), delay)
  }

  private frame(data: unknown) {
    this.arm()
    if (this.resync) {
      this.resync = false
      this.emit({ type: 'resync' })
    }
    let msg: { type?: unknown }
    try {
      msg = JSON.parse(String(data))
    } catch {
      return
    }
    switch (msg?.type) {
      case 'hello':
        this.attempt = 0
        this.set('open')
        return
      case 'session_updated':
      case 'device_synced':
      case 'reindex':
        this.emit(msg as LiveEvent)
    }
  }

  private emit(e: LiveEvent) {
    for (const f of this.events) f(e)
  }
}
