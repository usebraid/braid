// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import type { ReactNode } from 'react'
import { BrowserRouter, Route, Routes } from 'react-router-dom'
import { LiveProvider } from './api/live'
import { MeProvider } from './api/session'
import { Shell } from './components/Shell'
import { ROUTES } from './routes'
import { Admin } from './views/Admin'
import { Audit } from './views/Audit'
import { Devices } from './views/Devices'
import { Failures } from './views/Failures'
import { LiveView } from './views/LiveView'
import { NotFound } from './views/NotFound'
import { Overview } from './views/Overview'
import { Search } from './views/Search'
import { Session } from './views/Session'
import { Settings } from './views/Settings'
import { Timeline } from './views/Timeline'

// The admin view checks is_admin itself.
const VIEWS: Record<string, ReactNode> = {
  '/timeline': <Timeline />,
  '/live': <LiveView />,
  '/failures': <Failures />,
  '/search': <Search />,
  '/devices': <Devices />,
  '/audit': <Audit />,
  '/admin': <Admin />,
  '/settings': <Settings />,
}

export function App() {
  return (
    <BrowserRouter>
      <MeProvider>
        <LiveProvider>
          <Routes>
            <Route element={<Shell />}>
              {ROUTES.map((r) =>
                r.path === '/' ? (
                  <Route key={r.path} index element={<Overview />} />
                ) : (
                  <Route key={r.path} path={r.path} element={VIEWS[r.path]} />
                ),
              )}
              <Route path="/sessions/:id" element={<Session />} />
              <Route path="*" element={<NotFound />} />
            </Route>
          </Routes>
        </LiveProvider>
      </MeProvider>
    </BrowserRouter>
  )
}
