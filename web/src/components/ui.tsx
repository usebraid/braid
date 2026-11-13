// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Class names and structure are adapted from ponytail (https://github.com/DietrichGebert/ponytail, hooks/band/board.ts @96608e0:
// kpis, the segmented control, chips and the theme button), Copyright (c) 2026 DietrichGebert, MIT License;
// the license text is in THIRD_PARTY_NOTICES.md.

import { type CSSProperties, type ReactNode, useEffect, useId, useRef, useState } from 'react'
import { getTheme, nextTheme, setTheme, type ThemeChoice } from '../theme/theme'
import { Sparkline } from './charts/Sparkline'

export function Card({ className, children }: { className?: string; children: ReactNode }) {
  return <div className={className ? `card ${className}` : 'card'}>{children}</div>
}

// colors[1] tints the card; both feed the sparkline. `delta` is the change against the previous period in percent.
export function KpiCard(p: {
  label: string
  value: string
  delta?: number | null
  note?: string
  estimated?: boolean
  colors: [string, string]
  values?: number[]
  title?: string
  testId?: string
}) {
  const id = useId().replace(/:/g, '')
  const d = p.delta
  return (
    <div
      className="card kpi"
      style={{ '--k': p.colors[1] } as CSSProperties}
      title={p.title}
      data-testid={p.testId && `kpi-${p.testId}`}
    >
      <div className="kpi-label">{p.label}</div>
      <div className="kpi-value num">
        <span>{p.value}</span>
        {d != null ? (
          <span className={d >= 0 ? 'delta up' : 'delta down'}>
            {d >= 0 ? '↑' : '↓'}
            {Math.abs(d)}%
          </span>
        ) : null}
        {p.estimated ? <span className="kpi-est">estimated</span> : null}
      </div>
      {p.note ? <div className="kpi-note">{p.note}</div> : null}
      {p.values ? <Sparkline values={p.values} colors={p.colors} id={`kpi${id}`} /> : null}
    </div>
  )
}

// A native <select> in the shared control style; the visible first option names the filter, aria-label names it for AT.
export function Select(p: {
  label: string
  value: string
  options: { value: string; label: string }[]
  onChange: (v: string) => void
  id?: string
}) {
  return (
    <span className="select">
      <select
        className="ctl"
        id={p.id}
        aria-label={p.id ? undefined : p.label}
        value={p.value}
        onChange={(e) => p.onChange(e.target.value)}
      >
        {p.options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
    </span>
  )
}

// A wide table scrolls sideways inside this; keyboard users reach the scroll through its tab stop.
export function TableWrap({ label, children }: { label: string; children: ReactNode }) {
  return (
    // biome-ignore lint/a11y/noNoninteractiveTabindex: a scrollable region must be focusable (WCAG 2.1.1, axe scrollable-region-focusable)
    <section className="table-wrap" aria-label={label} tabIndex={0}>
      {children}
    </section>
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

const initials = (name: string) =>
  name
    .split(/[\s@._-]+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((w) => w[0]?.toUpperCase())
    .join('') || '?'

// The IdP picture, proxied by the server (the CSP allows images only from 'self'); a 404 or any load error falls back
// to initials on a palette colour picked from the name.
export function Avatar({ name }: { name: string }) {
  const [failed, setFailed] = useState(false)
  let h = 0
  for (const c of name) h = (h * 31 + c.charCodeAt(0)) >>> 0
  return (
    <span className="avatar" style={{ '--c': `var(--p${(h % 8) + 1})` } as CSSProperties} aria-hidden="true">
      {failed ? (
        initials(name)
      ) : (
        // biome-ignore lint/a11y/noNoninteractiveElementInteractions: onError is the image's load failure, not a user interaction
        <img src="/api/v1/me/avatar" alt="" onError={() => setFailed(true)} />
      )}
    </span>
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

// Modal confirm built on <dialog>; render it only while open. Esc and the Cancel button call onCancel.
export function ConfirmDialog(p: {
  title: string
  confirmLabel: string
  onConfirm: () => void
  onCancel: () => void
  disabled?: boolean
  children?: ReactNode
}) {
  const ref = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    const el = ref.current
    const opener = document.activeElement
    if (el && !el.open) typeof el.showModal === 'function' ? el.showModal() : el.setAttribute('open', '')
    return () => {
      if (opener instanceof HTMLElement) opener.focus() // unmounting drops focus to <body> otherwise
    }
  }, [])
  return (
    <dialog ref={ref} className="card confirm" aria-label={p.title} onCancel={p.onCancel}>
      <p>{p.title}</p>
      {p.children}
      <div className="row">
        <button type="button" className="btn" onClick={p.onCancel}>
          Cancel
        </button>
        <button type="button" className="btn" disabled={p.disabled} onClick={p.onConfirm}>
          {p.confirmLabel}
        </button>
      </div>
    </dialog>
  )
}
