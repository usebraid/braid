// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Adapted from ponytail (https://github.com/DietrichGebert/ponytail, hooks/band/board.ts @96608e0), Copyright (c) 2026 DietrichGebert,
// MIT License; the license text is in THIRD_PARTY_NOTICES.md.

import type { CSSProperties, ReactNode } from 'react'

type Row = { key: string; label: string; value: number; text: string; color: string; note?: ReactNode }

export function BarList(p: { title: string; rows: Row[] }) {
  const max = Math.max(0, ...p.rows.map((r) => r.value))
  return (
    <ul className="barlist" aria-label={p.title}>
      {p.rows.map((r) => {
        const share = max > 0 ? r.value / max : 0
        return (
          <li key={r.key}>
            <div className="bar-head">
              <span className="bar-label">{r.label}</span>
              {r.note ? <span className="sub">{r.note}</span> : null}
              <span className="num bar-text">{r.text}</span>
            </div>
            <div
              className="bar"
              data-share={share}
              style={{ '--c': r.color, width: `${share * 100}%` } as CSSProperties}
            />
          </li>
        )
      })}
    </ul>
  )
}
