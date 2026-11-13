// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Class names and structure are adapted from ponytail (https://github.com/DietrichGebert/ponytail, hooks/band/board.ts @96608e0:
// kpis, the segmented control, chips and the theme button), Copyright (c) 2026 DietrichGebert, MIT License;
// the license text is in THIRD_PARTY_NOTICES.md.

import { type CSSProperties, type ReactNode, useState } from 'react'
import { getTheme, nextTheme, setTheme, type ThemeChoice } from '../theme/theme'

export function Card({ className, children }: { className?: string; children: ReactNode }) {
  return <div className={className ? `card ${className}` : 'card'}>{children}</div>
}

// colors[1] tints the card; both feed the sparkline once Task 13 wires values.
export function KpiCard(p: {
  label: string
  value: string
  note?: string
  colors: [string, string]
  values?: number[]
  title?: string
  testId?: string
}) {
  return (
    <div className="card kpi" style={{ '--k': p.colors[1] } as CSSProperties} title={p.title} data-testid={p.testId}>
      <div className="kpi-label">{p.label}</div>
      <div className="kpi-value num">{p.value}</div>
      {p.note ? <div className="kpi-note">{p.note}</div> : null}
    </div>
  )
}

export function Pill({ kind, children }: { kind?: 'ok' | 'bad' | 'warn'; children: ReactNode }) {
  return (
    <span className={kind ? `pill ${kind}` : 'pill'}>
      <i />
      {children}
    </span>
  )
}

export function Segmented<T extends string>(p: {
  label: string
  value: T
  options: { value: T; label: string }[]
  onChange: (v: T) => void
}) {
  return (
    // biome-ignore lint/a11y/useSemanticElements: a button row styled as a segmented control, role=group is the contract
    <div className="seg" role="group" aria-label={p.label}>
      {p.options.map((o) => (
        <button
          key={o.value}
          type="button"
          className={o.value === p.value ? 'on' : undefined}
          aria-pressed={o.value === p.value}
          onClick={() => p.onChange(o.value)}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

export function Chip(p: { pressed: boolean; onClick: () => void; color?: string; children: ReactNode }) {
  return (
    <button type="button" className="chip" aria-pressed={p.pressed} onClick={p.onClick}>
      {p.color ? <span className="dot" style={{ '--c': p.color } as CSSProperties} /> : null}
      {p.children}
    </button>
  )
}

const ICONS: Record<ThemeChoice, ReactNode> = {
  system: (
    <>
      <rect x="3" y="4" width="18" height="12" rx="2" />
      <path d="M8 20h8M12 16v4" />
    </>
  ),
  light: (
    <>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
    </>
  ),
  dark: <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z" />,
}

export function ThemeButton() {
  const [choice, setChoice] = useState(getTheme)
  const next = nextTheme(choice)
  return (
    <button
      type="button"
      className="theme"
      aria-label={`Theme: ${choice}, switch to ${next}`}
      title={`Theme: ${choice}`}
      onClick={() => {
        setTheme(next)
        setChoice(next)
      }}
    >
      <svg
        width="18"
        height="18"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        {ICONS[choice]}
      </svg>
    </button>
  )
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="empty-note">{children}</div>
}

export function ErrorNote({ error, retry }: { error: unknown; retry?: () => void }) {
  return (
    <div className="card err" role="alert">
      <span>{error instanceof Error ? error.message : String(error)}</span>
      {retry ? (
        <button type="button" className="btn" onClick={retry}>
          Retry
        </button>
      ) : null}
    </div>
  )
}
