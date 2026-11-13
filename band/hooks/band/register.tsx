// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors
// Adapted from ponytail's band (hooks/band/register.tsx, MIT, Copyright (c) 2026 DietrichGebert): the
// register/session.start/ui.render(AbovePrompt) shape, slot stacking and atom state. See THIRD_PARTY_NOTICES.md.
//
// Zero tokens: this module only runs `braid stats --json` and draws a row. It registers no command, tool or
// prompt hook, writes no message, toast or status, and adds nothing to the model's context.
import type { EngineInterface, Register } from 'claude-code'
import { atom, read, update } from 'claude-code'
import { due, parseStats, view } from './stats'
import type { BandState } from './types'

const band = atom({ plugin: 'braid', key: 'band' } as const, {} as BandState)

// Runs the CLI when a run is due. Claims the slot first so parallel renders do not spawn twice.
async function refresh($: EngineInterface, force: boolean): Promise<void> {
  const now = await $.clock.now()
  let claimed = false
  await update($, band, (s) => {
    if (!due(s.lastRun, now, force)) return s
    claimed = true
    return { ...s, lastRun: now }
  })
  if (!claimed) return
  const id = await $.session.id().catch(() => undefined)
  const ran = await $.process.run(['braid', 'stats', '--json', ...(id ? ['--session', id] : [])]).catch(() => undefined)
  // Missing binary, failure or garbage all end as "not connected": no error text anywhere.
  const stats = ran && ran.exitCode === 0 ? parseStats(ran.stdout) : undefined
  await update($, band, (s) => ({ ...s, stats }))
}

export const register: Register = (on) => {
  on('session.start', async ($: EngineInterface, e: unknown, next: (e: unknown) => unknown) => {
    await refresh($, true).catch(() => undefined)
    return next(e)
  }).catch((_$: EngineInterface, e: unknown, next: (e: unknown) => unknown) => next(e))

  on(
    'ui.render',
    { component: 'AbovePrompt' },
    // biome-ignore lint/suspicious/noExplicitAny: the render event is typed by Claude Code.
    async ($: EngineInterface, e: any, next: (e: unknown) => Promise<unknown>) => {
      if (e.props.hasSurvey) return next(e)
      const { stats, lastRun } = await read($, band)
      const now = await $.clock.now()
      // Never awaited: rendering does not wait for the CLI.
      if (due(lastRun, now, false)) void refresh($, false).catch(() => undefined)
      // $.session.usage() has no token total (only cost and the context window), so the session shows cost only.
      const usage = await $.session.usage().catch(() => undefined)
      const v = view(stats, usage?.cost ? { cost: usage.cost.usd } : undefined, now)
      if (v.kind === 'hidden') return next(e)

      // Other mods share this slot: draw theirs below ours. next(e) rejects when nothing answers beneath.
      const below = await next(e).catch(() => undefined)
      const { Box, Text } = $.ui.resolve(e)
      const mine =
        v.kind === 'not_connected' ? (
          <Text dimColor>{v.text}</Text>
        ) : (
          <Box flexDirection="row" gap={1}>
            {v.segments.map((s, i) => (
              <Text key={i} color={s.color} dimColor={s.dim}>
                {s.text}
              </Text>
            ))}
          </Box>
        )
      return below ? (
        <Box flexDirection="column">
          {mine}
          {below}
        </Box>
      ) : (
        mine
      )
    },
  ).catch((_$: EngineInterface, e: unknown, next: (e: unknown) => unknown) => next(e))
}
