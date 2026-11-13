// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { useEffect, useRef } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router-dom'
import { useMe } from '../api/session'
import { ROUTES, titleFor } from '../routes'
import { Footer } from './Footer'
import { ThemeButton } from './ui'

export function Shell() {
  const me = useMe()
  const { pathname } = useLocation()
  const main = useRef<HTMLElement>(null)
  const first = useRef(true)
  useEffect(() => {
    document.title = `${titleFor(pathname)} · Braid`
    if (first.current) first.current = false
    else main.current?.focus()
  }, [pathname])
  return (
    <div className="app">
      <a className="skip" href="#main">
        Skip to content
      </a>
      <aside className="side">
        <div className="brand">
          <svg
            width="22"
            height="22"
            viewBox="0 0 24 24"
            fill="none"
            stroke="var(--p1)"
            strokeWidth="2"
            strokeLinecap="round"
            aria-hidden="true"
          >
            <path d="M5 3c0 6 14 6 14 12M12 3c0 6 7 6 7 12M5 9c0 6 7 6 7 12" />
          </svg>
          Braid
        </div>
        <nav className="nav-list" aria-label="Main">
          {ROUTES.filter((r) => !r.adminOnly || me.is_admin).map((r) => (
            <NavLink key={r.path} to={r.path} end className={({ isActive }) => (isActive ? 'nav-item on' : 'nav-item')}>
              {r.label}
            </NavLink>
          ))}
        </nav>
        <div className="side-foot">
          <span className="foot-text">{me.name || me.email}</span>
          <ThemeButton />
        </div>
      </aside>
      <div>
        <main id="main" tabIndex={-1} ref={main}>
          <Outlet />
        </main>
        <Footer />
      </div>
    </div>
  )
}
