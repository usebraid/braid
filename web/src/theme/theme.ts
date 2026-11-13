// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// The dark/light/system cycle is adapted from ponytail (hooks/band/board.ts @96608e0: THEMES, applyTheme),
// Copyright (c) 2026 DietrichGebert, MIT License; the license text is in THIRD_PARTY_NOTICES.md.

export type ThemeChoice = 'dark' | 'light' | 'system'

const CHOICES: ThemeChoice[] = ['dark', 'light', 'system']
const KEY = 'braid-theme'

export function getTheme(): ThemeChoice {
  try {
    const v = localStorage.getItem(KEY)
    if (CHOICES.includes(v as ThemeChoice)) return v as ThemeChoice
  } catch {
    // storage blocked: fall through to the default
  }
  return 'system'
}

export function setTheme(c: ThemeChoice): void {
  document.documentElement.setAttribute('data-theme', c)
  try {
    localStorage.setItem(KEY, c)
  } catch {
    // storage blocked: the choice lasts for this page only
  }
}

export function nextTheme(c: ThemeChoice): ThemeChoice {
  return CHOICES[(CHOICES.indexOf(c) + 1) % CHOICES.length] as ThemeChoice
}
