// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { BrowserRouter, Route, Routes } from 'react-router-dom'
import { MeProvider, useMe } from './api/session'
import { Shell } from './components/Shell'
import { ROUTES } from './routes'
import { NotFound } from './views/NotFound'
import { Placeholder } from './views/Placeholder'

function AdminOnly({ children }: { children: React.ReactNode }) {
  return useMe().is_admin ? children : <NotFound />
}

export function App() {
  return (
    <BrowserRouter>
      <MeProvider>
        <Routes>
          <Route element={<Shell />}>
            {ROUTES.map((r) =>
              r.path === '/' ? (
                <Route key={r.path} index element={<Placeholder title={r.label} />} />
              ) : (
                <Route
                  key={r.path}
                  path={r.path}
                  element={
                    r.adminOnly ? (
                      <AdminOnly>
                        <Placeholder title={r.label} />
                      </AdminOnly>
                    ) : (
                      <Placeholder title={r.label} />
                    )
                  }
                />
              ),
            )}
            <Route path="/sessions/:id" element={<Placeholder title="Session" />} />
            <Route path="*" element={<NotFound />} />
          </Route>
        </Routes>
      </MeProvider>
    </BrowserRouter>
  )
}
