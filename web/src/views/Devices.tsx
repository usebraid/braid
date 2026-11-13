// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { useState } from 'react'
import { useApi } from '../api/hooks'
import { renameDevice, revokeDevice } from '../api/manage'
import { useMe } from '../api/session'
import { listDevices } from '../api/stats'
import type { Device } from '../api/types'
import { Card, ConfirmDialog, Empty, ErrorNote, Pill } from '../components/ui'
import { fmtDateTime } from '../lib/format'

const ago = (iso: string | null) => {
  if (!iso) return 'never'
  const h = Math.max(0, Math.floor((Date.now() - new Date(iso).getTime()) / 3_600_000))
  return h < 48 ? `${h}h ago` : `${Math.floor(h / 24)}d ago`
}

export function Devices() {
  const me = useMe()
  const { data, error, reload } = useApi('devices', (s) => listDevices(s), {
    on: (e) => e.type === 'device_synced',
  })
  const [editing, setEditing] = useState<{ id: string; name: string } | null>(null)
  const [revoking, setRevoking] = useState<Device | null>(null)
  const [failure, setFailure] = useState<unknown>()
  const run = (p: Promise<unknown>) =>
    p.then(
      () => {
        setFailure(undefined)
        reload()
      },
      (e: unknown) => setFailure(e),
    )
  const save = () => {
    if (editing?.name.trim()) void run(renameDevice(editing.id, editing.name.trim()))
    setEditing(null)
  }
  const when = (iso: string | null) => (iso ? fmtDateTime(iso, me.time_zone) : 'never')
  return (
    <>
      <div className="top">
        <h1>Devices</h1>
      </div>
      {error && !data ? <ErrorNote error={error} retry={reload} /> : null}
      {failure ? <ErrorNote error={failure} /> : null}
      {data?.length === 0 ? <Empty>No devices yet</Empty> : null}
      {data?.length ? (
        <Card>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Hostname</th>
                  <th>OS</th>
                  <th>User</th>
                  <th>Daemon</th>
                  <th>Last sync</th>
                  <th>Backlog</th>
                  <th>Last seen</th>
                  <th>State</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {data.map((d) => (
                  <tr key={d.id} className={d.revoked_at ? 'faded' : undefined}>
                    <td>
                      {editing?.id === d.id ? (
                        <input
                          aria-label="Device name"
                          // biome-ignore lint/a11y/noAutofocus: the input appears on an explicit Rename click
                          autoFocus
                          value={editing.name}
                          onChange={(e) => setEditing({ id: d.id, name: e.target.value })}
                          onKeyDown={(e) => {
                            if (e.key === 'Enter') save()
                            if (e.key === 'Escape') setEditing(null)
                          }}
                        />
                      ) : (
                        d.name
                      )}
                    </td>
                    <td>{d.hostname}</td>
                    <td>{`${d.os}/${d.arch}`}</td>
                    <td>{d.os_user}</td>
                    <td>{d.daemon_version}</td>
                    <td>{when(d.last_sync_at)}</td>
                    <td>{ago(d.last_sync_at)}</td>
                    <td>{when(d.last_seen_at)}</td>
                    <td>
                      {d.revoked_at ? (
                        <Pill kind="bad">revoked</Pill>
                      ) : d.stale ? (
                        <Pill kind="warn">stale: no sync for 7+ days</Pill>
                      ) : (
                        <Pill kind="ok">active</Pill>
                      )}
                    </td>
                    <td>
                      {d.revoked_at ? null : (
                        <div className="actions">
                          <button
                            type="button"
                            className="btn"
                            aria-label={`Rename ${d.name}`}
                            onClick={() => setEditing({ id: d.id, name: d.name })}
                          >
                            Rename
                          </button>
                          <button
                            type="button"
                            className="btn"
                            aria-label={`Revoke ${d.name}`}
                            onClick={() => setRevoking(d)}
                          >
                            Revoke
                          </button>
                        </div>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <p className="sub">The daemon reports its backlog locally (`braid status`).</p>
        </Card>
      ) : null}
      {revoking ? (
        <ConfirmDialog
          title={`Revoke ${revoking.name}? Its tokens stop working immediately; synced data stays.`}
          confirmLabel="Confirm revoke"
          onCancel={() => setRevoking(null)}
          onConfirm={() => {
            void run(revokeDevice(revoking.id))
            setRevoking(null)
          }}
        />
      ) : null}
    </>
  )
}
