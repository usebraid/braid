// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { useEffect, useState } from 'react'
import { getTheme, type ThemeChoice } from './theme'

// Follows the theme toggle: setTheme writes data-theme on <html>, which this observes.
export function useTheme(): ThemeChoice {
  const [t, setT] = useState(getTheme)
  useEffect(() => {
    const mo = new MutationObserver(() => setT(getTheme()))
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] })
    return () => mo.disconnect()
  }, [])
  return t
}
