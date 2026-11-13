// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { describe, expect, it } from 'vitest'
import { highlight } from './highlight'

describe('highlight', () => {
  it('marks query words case-insensitively', () => {
    expect(highlight('Zebra crossing quokka', 'zebra quokka')).toEqual([
      { text: 'Zebra', hit: true },
      { text: ' crossing ', hit: false },
      { text: 'quokka', hit: true },
    ])
  })
  it('escapes regex characters and ignores one-letter words', () => {
    expect(highlight('a.b (c)', '(c) a')).toEqual([
      { text: 'a.b ', hit: false },
      { text: '(c)', hit: true },
    ])
    expect(highlight('plain', 'zzz')).toEqual([{ text: 'plain', hit: false }])
  })
})
