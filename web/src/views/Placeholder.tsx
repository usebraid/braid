// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { Empty } from '../components/ui'

// Stands in for a view until its task lands.
export function Placeholder({ title }: { title: string }) {
  return (
    <>
      <div className="top">
        <h1>{title}</h1>
      </div>
      <Empty>Coming soon.</Empty>
    </>
  )
}
