// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { render } from '@testing-library/react'
import { expect, it } from 'vitest'
import { Markdown } from './Markdown'

it('renders a fenced code block and a table', () => {
  const { container } = render(<Markdown text={'```go\nfmt.Println(1)\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |\n'} />)
  expect(container.querySelector('pre code')?.textContent).toBe('fmt.Println(1)\n')
  expect(container.querySelectorAll('.md-scroll table th')).toHaveLength(2)
})

it('renders raw HTML as text and links safely', () => {
  const { container } = render(
    <Markdown text={'hi <script>alert(1)</script> <b>x</b> [l](https://a.dev) [j](javascript:alert(1))'} />,
  )
  expect(container.querySelector('script, b')).toBeNull()
  expect(container.textContent).toContain('hi <script>alert(1)</script>')
  const a = container.querySelector('a[href="https://a.dev"]')
  expect(a).toHaveAttribute('target', '_blank')
  expect(a).toHaveAttribute('rel', 'noopener noreferrer')
  expect(container.querySelector('a[href^="javascript"]')).toBeNull()
})
