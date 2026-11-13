// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors
// Generates src/api/schema.d.ts from ../api/openapi.yaml; with --check only compares.
import { readFileSync, writeFileSync } from 'node:fs'
import openapiTS, { astToString } from 'openapi-typescript'

const spec = new URL('../../api/openapi.yaml', import.meta.url)
const out = new URL('../src/api/schema.d.ts', import.meta.url)
const header = `${[
  '// SPDX-License-Identifier: AGPL-3.0-or-later',
  '// Copyright (C) 2026 The Braid Authors',
  '// Code generated from api/openapi.yaml; DO NOT EDIT.',
].join('\n')}\n\n`

const generated = header + astToString(await openapiTS(spec))

if (process.argv.includes('--check')) {
  let current = ''
  try {
    current = readFileSync(out, 'utf8')
  } catch {}
  if (current !== generated) {
    console.error('src/api/schema.d.ts is out of date with api/openapi.yaml; run "npm run gen:api"')
    process.exit(1)
  }
} else {
  writeFileSync(out, generated)
}
