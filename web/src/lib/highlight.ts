// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

export type Part = { text: string; hit: boolean }

// Splits text into parts around the query words (>= 2 chars), case-insensitive; never touches HTML.
export function highlight(text: string, query: string): Part[] {
  const words = [...new Set(query.split(/\s+/).filter((w) => w.length >= 2))]
    .sort((a, b) => b.length - a.length)
    .map((w) => w.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'))
  if (words.length === 0) return [{ text, hit: false }]
  const re = new RegExp(`(${words.join('|')})`, 'gi')
  const parts: Part[] = []
  text.split(re).forEach((t, i) => {
    if (!t) return
    const hit = i % 2 === 1 // split with one capture group alternates miss, hit
    const last = parts[parts.length - 1]
    if (last && last.hit === hit) last.text += t
    else parts.push({ text: t, hit })
  })
  return parts.length ? parts : [{ text, hit: false }]
}
