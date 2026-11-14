// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { memo } from 'react'
import { toolSummary } from '../../lib/toolSummary'
import type { Entry } from '../../lib/transcript'
import { Pill } from '../ui'
import { color } from './GraphCell'
import { Markdown } from './Markdown'

// Text only, never markup; same rule as the transcript panel.
type Props = { e: Entry; tag?: string; lane?: number }

// buildRows makes new Entry objects on every page load; an entry's content never changes, so compare by value and
// skip re-parsing the markdown of every row already shown.
export const ChatEntry = memo(ChatEntryView, (a: Props, b: Props) => {
  const x = a.e
  const y = b.e
  return (
    a.tag === b.tag &&
    a.lane === b.lane &&
    x.lineId === y.lineId &&
    x.kind === y.kind &&
    x.text === y.text &&
    x.truncated === y.truncated &&
    x.isError === y.isError
  )
})

function ChatEntryView({ e, tag, lane = 0 }: Props) {
  const lead = tag ? <span className="gtag">{tag}</span> : null
  const more = e.truncated ? '\n… truncated' : ''
  const text = (
    <pre className="entry-text">
      {e.text}
      {more}
    </pre>
  )
  const prose = (
    <div className="g-text">
      {lead}
      <Markdown text={e.text + more} />
    </div>
  )
  switch (e.kind) {
    case 'prompt':
      // the first prompt of a subagent lane is the parent's task brief, not the user speaking
      return tag ? (
        <div className="g-brief" style={{ borderLeftColor: color(lane) }}>
          {prose}
        </div>
      ) : (
        <div className="g-prompt">{prose}</div>
      )
    case 'reply':
      return <div className="g-reply">{prose}</div>
    case 'tool_call':
      return (
        <div className="g-tool">
          {lead}
          <b>{e.tool?.name || 'Tool'}</b>{' '}
          <span>{(e.tool?.input !== undefined ? toolSummary(e.tool.name, e.tool.input) : '') || e.text}</span>
        </div>
      )
    case 'thinking':
    case 'tool_result':
      return (
        <details className="g-fold">
          <summary>
            {lead}
            {e.kind === 'thinking' ? 'Thinking' : 'Result'}
            {e.isError ? <Pill kind="bad">error</Pill> : null}
            <span className="g-peek">{e.text.slice(0, 80)}</span>
          </summary>
          {text}
        </details>
      )
    default:
      return (
        <div className="g-note">
          {lead}
          {e.isError ? <Pill kind="bad">failed</Pill> : null} {e.text}
        </div>
      )
  }
}
