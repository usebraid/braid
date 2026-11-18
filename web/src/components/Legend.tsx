// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { useState } from 'react'
import type { LegendItem, Shape } from '../lib/legend'

const KEY = 'braid.legend.collapsed'
const read = () => {
  try {
    return localStorage.getItem(KEY) === '1'
  } catch {
    return false
  }
}

function Swatch({ shape, color }: { shape: Shape; color: string }) {
  const c = { stroke: color, fill: 'none' }
  return (
    <svg width="16" height="12" viewBox="0 0 16 12" aria-hidden="true" focusable="false">
      {shape === 'dot' ? <circle cx="8" cy="6" r="4" style={{ stroke: color, fill: color }} /> : null}
      {shape === 'ring' ? (
        <circle cx="8" cy="6" r="3.5" style={{ ...c, fill: 'var(--card-solid)', strokeWidth: 1.5 }} />
      ) : null}
      {shape === 'square' ? (
        <rect x="4" y="2" width="8" height="8" rx="1" style={{ stroke: color, fill: color }} />
      ) : null}
      {shape === 'diamond' ? (
        <polygon points="8,0.5 13.5,6 8,11.5 2.5,6" style={{ stroke: color, fill: color }} />
      ) : null}
      {shape === 'line' ? <line x1="2" x2="14" y1="6" y2="6" style={c} /> : null}
      {shape === 'dotted' ? <line className="gdotted" x1="2" x2="14" y1="6" y2="6" style={c} /> : null}
      {shape === 'dashed' ? (
        <line x1="1" x2="15" y1="6" y2="6" style={{ ...c, strokeWidth: 1, strokeDasharray: '3 2' }} />
      ) : null}
      {shape === 'box' ? (
        <rect x="1.5" y="2.5" width="13" height="7" rx="2" style={{ ...c, strokeWidth: 1.5 }} />
      ) : null}
      {shape === 'boxDashed' ? (
        <rect x="1.5" y="2.5" width="13" height="7" rx="2" style={{ ...c, strokeWidth: 1.5, strokeDasharray: '3 2' }} />
      ) : null}
      {shape === 'bar' ? (
        <rect x="2" y="4.5" width="12" height="3" rx="1.5" style={{ stroke: color, fill: color }} />
      ) : null}
    </svg>
  )
}

// Names only the types in the current view. The collapsed state is per browser.
export function Legend({ items }: { items: LegendItem[] }) {
  const [collapsed, setCollapsed] = useState(read)
  if (items.length === 0) return null
  const flip = () => {
    const next = !collapsed
    setCollapsed(next)
    try {
      localStorage.setItem(KEY, next ? '1' : '0')
    } catch {
      /* private mode or blocked storage: the choice just does not persist */
    }
  }
  return (
    <section className="legend" aria-label="Legend">
      <button type="button" aria-expanded={!collapsed} onClick={flip}>
        {collapsed ? '▸' : '▾'} Legend
      </button>
      {collapsed ? null : (
        <ul>
          {items.map((i) => (
            <li key={i.key} data-legend={i.key}>
              <Swatch shape={i.shape} color={i.color} />
              {i.label}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
