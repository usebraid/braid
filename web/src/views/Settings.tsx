// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { useState } from 'react'
import { setTimeZone } from '../api/manage'
import { useMe } from '../api/session'
import { Card, ErrorNote, Segmented } from '../components/ui'
import { getTheme, setTheme, type ThemeChoice } from '../theme/theme'

const browserZone = Intl.DateTimeFormat().resolvedOptions().timeZone

export function Settings() {
  const me = useMe()
  const [zone, setZone] = useState(me.time_zone)
  const [theme, setThemeState] = useState<ThemeChoice>(getTheme)
  const [error, setError] = useState<unknown>()
  const [saved, setSaved] = useState(false)
  const zones = [...new Set(['UTC', ...Intl.supportedValuesOf('timeZone'), me.time_zone])]
  const save = () =>
    setTimeZone(zone).then(
      () => {
        setError(undefined)
        setSaved(true)
        me.refresh()
      },
      (e: unknown) => setError(e),
    )
  return (
    <>
      <div className="top">
        <h1>Settings</h1>
      </div>
      <Card>
        <h2>Time zone</h2>
        <div className="field">
          <label htmlFor="tz">Time zone</label>
          <select
            id="tz"
            value={zone}
            onChange={(e) => {
              setZone(e.target.value)
              setSaved(false)
            }}
          >
            {zones.map((z) => (
              <option key={z} value={z}>
                {z}
              </option>
            ))}
          </select>
        </div>
        <div className="actions">
          {browserZone !== zone ? (
            <button type="button" className="btn" onClick={() => setZone(browserZone)}>
              {`Use browser zone (${browserZone})`}
            </button>
          ) : null}
          <button type="button" className="btn" disabled={zone === me.time_zone && me.time_zone_set} onClick={save}>
            Save
          </button>
          {saved ? <span className="sub">Saved</span> : null}
        </div>
        {error ? <ErrorNote error={error} /> : null}
      </Card>
      <Card>
        <h2>Theme</h2>
        <Segmented
          label="Theme"
          value={theme}
          options={[
            { value: 'system', label: 'System' },
            { value: 'dark', label: 'Dark' },
            { value: 'light', label: 'Light' },
          ]}
          onChange={(v) => {
            setTheme(v)
            setThemeState(v)
          }}
        />
      </Card>
    </>
  )
}
