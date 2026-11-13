// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import type { Entry } from '../../lib/transcript'
import { Pill } from '../ui'

// Text only, never markup; same rule as the transcript panel.
export function ChatEntry({ e, tag }: { e: Entry; tag?: string }) {
  const text = (
    <pre className="entry-text">
      {e.text}
      {e.truncated ? '\n… truncated' : ''}
    </pre>
  )
  const lead = tag ? <span className="gtag">{tag}</span> : null
  switch (e.kind) {
    case 'prompt':
      return (
        <div className="g-prompt">
          {lead}
          {text}
        </div>
      )
    case 'reply':
      return (
        <div className="g-reply">
          {lead}
          {text}
        </div>
      )
    case 'tool_call':
      return (
        <div className="g-tool">
          {lead}
          <b>{e.tool?.name || 'Tool'}</b> <span>{e.text}</span>
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
          {e.text}
        </div>
      )
  }
}
