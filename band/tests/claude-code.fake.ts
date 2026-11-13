// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors
// Stand-in for Claude Code's 'claude-code' module: the real one exists only inside Claude Code.
// atom/read/update keep their values in `$.state`, as the engine does per session.
export type Atom<T> = { ref: { plugin: string; key: string }; initial: T }
export type EngineInterface = any
export type Register = (on: any, options: { readonly [field: string]: unknown }) => void
export interface PluginState {
  [plugin: string]: any
}

const slot = (a: Atom<unknown>) => `${a.ref.plugin}/${a.ref.key}`
export const atom = <T>(ref: { plugin: string; key: string }, initial: T): Atom<T> => ({ ref, initial })
export const read = async <T>($: EngineInterface, a: Atom<T>): Promise<T> =>
  ($.state[slot(a)] as T | undefined) ?? a.initial
export const update = async <T>($: EngineInterface, a: Atom<T>, fn: (v: T) => T): Promise<T> => {
  const v = fn(await read($, a))
  $.state[slot(a)] = v
  return v
}

declare global {
  // The JSX factory Claude Code compiles against; here it builds plain data.
  function h(type: unknown, props: unknown, ...children: unknown[]): unknown
  const Fragment: unknown
  namespace JSX {
    type Element = unknown
    interface IntrinsicElements {
      [tag: string]: any
    }
  }
}

const g = globalThis as Record<string, unknown>
g.h = (type: unknown, props: Record<string, unknown> | null, ...children: unknown[]) => ({
  type,
  props: props ?? {},
  children,
})
g.Fragment = 'Fragment'
