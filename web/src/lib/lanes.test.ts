// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { describe, expect, it } from 'vitest'
import { assignLanes, type LaneAgent, MAX_LANES } from './lanes'

const ag = (id: string, parent: string | null, from: string, to: string | null): LaneAgent => ({ id, parent, from, to })

describe('assignLanes', () => {
  it('reuses a lane for sequential agents', () => {
    const l = assignLanes([ag('main', null, '', null), ag('a', 'main', '01', '02'), ag('b', 'main', '03', '04')])
    expect([l.get('main'), l.get('a'), l.get('b')]).toEqual([0, 1, 1])
  })
  it('gives parallel agents separate lanes', () => {
    const l = assignLanes([ag('a', 'main', '01', '05'), ag('b', 'main', '02', '04')])
    expect([l.get('a'), l.get('b')]).toEqual([1, 2])
  })
  it('puts a nested agent right of its parent', () => {
    const l = assignLanes([ag('a', 'main', '01', '05'), ag('c', 'a', '02', '03')])
    expect(l.get('c')).toBe(2)
  })
  it('never exceeds MAX_LANES', () => {
    const many = Array.from({ length: 102 }, (_, i) => ag(`x${i}`, 'main', String(i).padStart(3, '0'), null))
    expect(Math.max(...assignLanes(many).values())).toBe(MAX_LANES - 1)
  })
})
