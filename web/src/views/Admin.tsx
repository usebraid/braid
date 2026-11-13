// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { useState } from 'react'
import { useApi } from '../api/hooks'
import { adminListAudit, adminStats, deleteUser, disableUser, enableUser } from '../api/manage'
import { useMe } from '../api/session'
import type { AdminUserStats } from '../api/types'
import { Card, ConfirmDialog, ErrorNote, KpiCard, Pill, TableWrap } from '../components/ui'
import { fmtBytes, fmtCost, fmtDateTime, fmtTokens } from '../lib/format'
import { AuditTable } from './Audit'
import { NotFound } from './NotFound'

type Pending = { kind: 'disable' | 'delete'; user: AdminUserStats }

export function Admin() {
  return useMe().is_admin ? <AdminView /> : <NotFound />
}

function AdminView() {
  const me = useMe()
  const { data, error, reload } = useApi('admin-stats', (s) => adminStats(s))
  const [pending, setPending] = useState<Pending | null>(null)
  const [typed, setTyped] = useState('')
  const [failure, setFailure] = useState<unknown>()
  const run = (p: Promise<unknown>) =>
    p.then(
      () => {
        setFailure(undefined)
        reload()
      },
      (e: unknown) => setFailure(e),
    )
  const when = (iso: string | null) => (iso ? fmtDateTime(iso, me.time_zone) : '–')
  const who = (id: string | null) => (id ? (data?.users.find((u) => u.id === id)?.email ?? id.slice(0, 8)) : '')
  const close = () => {
    setPending(null)
    setTyped('')
  }
  const t = data?.totals
  return (
    <>
      <div className="top">
        <h1>Admin</h1>
      </div>
      {error && !data ? <ErrorNote error={error} retry={reload} /> : null}
      {failure ? <ErrorNote error={failure} /> : null}
      {t ? (
        <div className="grid-kpi">
          <KpiCard label="Users" value={String(t.users)} colors={['var(--p1)', 'var(--p2)']} />
          <KpiCard label="Devices" value={String(t.devices)} colors={['var(--p3)', 'var(--p4)']} />
          <KpiCard label="Sessions" value={String(t.sessions)} colors={['var(--p5)', 'var(--p6)']} />
          <KpiCard label="Stored" value={fmtBytes(t.stored_bytes)} colors={['var(--p7)', 'var(--p8)']} />
          <KpiCard label="Cost" value={fmtCost(t.cost_usd)} colors={['var(--p1)', 'var(--p3)']} />
          <KpiCard label="Tokens" value={fmtTokens(t.tokens)} colors={['var(--p2)', 'var(--p4)']} />
        </div>
      ) : null}
      {data ? (
        <Card>
          <TableWrap label="Users">
            <table>
              <thead>
                <tr>
                  <th>User</th>
                  <th className="r">Sessions</th>
                  <th className="r">Cost</th>
                  <th className="r">Tokens</th>
                  <th className="r">Devices</th>
                  <th className="r">Stored</th>
                  <th>Last login</th>
                  <th>Last sync</th>
                  <th>Last activity</th>
                  <th>State</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {data.users.map((u) => (
                  <tr key={u.id} className={u.disabled_at ? 'faded' : undefined}>
                    <td>
                      {u.name !== u.email ? <div>{u.name}</div> : null}
                      <div className={u.name !== u.email ? 'sub' : undefined}>{u.email}</div>
                      {u.is_admin ? <Pill>admin</Pill> : null}
                    </td>
                    <td className="num r">{u.sessions}</td>
                    <td className="num r">{fmtCost(u.cost_usd)}</td>
                    <td className="num r">{fmtTokens(u.tokens)}</td>
                    <td className="num r">{u.devices}</td>
                    <td className="num r">{fmtBytes(u.stored_bytes)}</td>
                    <td className="num">{when(u.last_login_at)}</td>
                    <td className="num">{when(u.last_sync_at)}</td>
                    <td className="num">{when(u.last_activity_at)}</td>
                    <td>
                      <div className="pills">
                        {u.disabled_at ? <Pill kind="bad">disabled</Pill> : null}
                        {u.rebuilding ? <Pill kind="warn">rebuilding</Pill> : null}
                        {u.derive_errors > 0 ? <Pill kind="bad">{`${u.derive_errors} derive errors`}</Pill> : null}
                      </div>
                    </td>
                    <td>
                      {u.id === me.id ? null : (
                        <div className="actions">
                          {u.disabled_at ? (
                            <button
                              type="button"
                              className="btn ghost"
                              aria-label={`Enable ${u.email}`}
                              onClick={() => void run(enableUser(u.id))}
                            >
                              Enable
                            </button>
                          ) : (
                            <button
                              type="button"
                              className="btn ghost"
                              aria-label={`Disable ${u.email}`}
                              onClick={() => setPending({ kind: 'disable', user: u })}
                            >
                              Disable
                            </button>
                          )}
                          <button
                            type="button"
                            className="btn ghost danger"
                            aria-label={`Delete ${u.email}`}
                            onClick={() => setPending({ kind: 'delete', user: u })}
                          >
                            Delete
                          </button>
                        </div>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </TableWrap>
        </Card>
      ) : null}
      {pending?.kind === 'disable' ? (
        <ConfirmDialog
          title={`Disable ${pending.user.email}? They are signed out and their devices stop syncing.`}
          confirmLabel="Confirm disable"
          onCancel={close}
          onConfirm={() => {
            void run(disableUser(pending.user.id))
            close()
          }}
        />
      ) : null}
      {pending?.kind === 'delete' ? (
        <ConfirmDialog
          title={`Delete ${pending.user.email} and all their data? This cannot be undone.`}
          confirmLabel="Confirm delete"
          disabled={typed !== pending.user.email}
          onCancel={close}
          onConfirm={() => {
            void run(deleteUser(pending.user.id, pending.user.email))
            close()
          }}
        >
          <div className="field">
            <label htmlFor="confirm-email">Type the email to confirm</label>
            <input
              className="ctl"
              id="confirm-email"
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              autoComplete="off"
            />
          </div>
        </ConfirmDialog>
      ) : null}
      <h2 className="section-title">Instance audit</h2>
      <AuditTable load={adminListAudit} who={who} />
    </>
  )
}
