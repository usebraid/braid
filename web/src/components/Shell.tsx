// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { type CSSProperties, useEffect, useRef, useState } from 'react'
import { Link, NavLink, Outlet, useLocation } from 'react-router-dom'
import { LiveBanner } from '../api/live'
import { useMe } from '../api/session'
import { ROUTES, titleFor } from '../routes'
import { Footer } from './Footer'
import { Avatar, ThemeButton } from './ui'

export function Shell() {
  const me = useMe()
  const { pathname } = useLocation()
  const main = useRef<HTMLElement>(null)
  const first = useRef(true)
  const [open, setOpen] = useState(false) // the mobile menu
  useEffect(() => {
    setOpen(false)
    document.title = `${titleFor(pathname)} · Braid`
    if (first.current) first.current = false
    else main.current?.focus()
  }, [pathname])
  return (
    <div className="app">
      <a className="skip" href="#main">
        Skip to content
      </a>
      <aside className={open ? 'side open' : 'side'}>
        <div className="side-top">
          <Link to="/" className="brand" aria-label="Braid, go to overview">
            <svg
              width="22"
              height="22"
              viewBox="0 0 24 24"
              fill="none"
              strokeWidth="2"
              strokeLinecap="round"
              aria-hidden="true"
            >
              <path d="M5 3c0 6 14 6 14 12" stroke="var(--p1)" />
              <path d="M12 3c0 6 7 6 7 12" stroke="var(--p2)" />
              <path d="M5 9c0 6 7 6 7 12" stroke="var(--p3)" />
            </svg>
            Braid
          </Link>
          <button
            type="button"
            className="menu-btn"
            aria-label="Menu"
            aria-expanded={open}
            aria-controls="nav"
            onClick={() => setOpen(!open)}
          >
            <svg
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.8"
              strokeLinecap="round"
              aria-hidden="true"
            >
              <path d={open ? 'M6 6l12 12M18 6L6 18' : 'M4 7h16M4 12h16M4 17h16'} />
            </svg>
          </button>
        </div>
        <nav className="nav-list" id="nav" aria-label="Main">
          {ROUTES.filter((r) => !r.adminOnly || me.is_admin).map((r, i) => (
            <NavLink
              key={r.path}
              to={r.path}
              end
              style={{ '--c': `var(--p${(i % 8) + 1})` } as CSSProperties}
              className={({ isActive }) => (isActive ? 'nav-item on' : 'nav-item')}
            >
              {r.label}
            </NavLink>
          ))}
        </nav>
        <div className="side-foot">
          <Avatar name={me.name || me.email} />
          <span className="foot-text">{me.name || me.email}</span>
          <ThemeButton />
          {/* a real form POST: /auth/logout checks the Origin header and must never be a GET link */}
          <form method="post" action="/auth/logout" className="signout">
            <button type="submit" className="btn ghost">
              Sign out
            </button>
          </form>
        </div>
      </aside>
      <div>
        <LiveBanner />
        <main id="main" tabIndex={-1} ref={main}>
          <Outlet />
        </main>
        <Footer />
      </div>
    </div>
  )
}
