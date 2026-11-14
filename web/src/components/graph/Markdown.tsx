// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { memo } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

const plugins = [remarkGfm]
const components = {
  a: ({ node: _n, ...p }: React.ComponentProps<'a'> & { node?: unknown }) => (
    <a {...p} target="_blank" rel="noopener noreferrer" />
  ),
  table: ({ node: _n, ...p }: React.ComponentProps<'table'> & { node?: unknown }) => (
    <div className="md-scroll">
      <table {...p} />
    </div>
  ),
}

// No rehype-raw: raw HTML in a transcript renders as text, never as markup.
export const Markdown = memo(function Markdown({ text }: { text: string }) {
  return (
    <div className="md">
      <ReactMarkdown remarkPlugins={plugins} components={components}>
        {text}
      </ReactMarkdown>
    </div>
  )
})
