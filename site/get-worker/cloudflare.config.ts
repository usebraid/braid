// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Project config for `cf deploy` (typed cloudflare.config.ts, no Wrangler file). Deploying is an owner step.
// Written without the `defineConfig` helper (an identity function) to avoid a dependency on the `cf` package.
export default {
  worker: {
    name: 'braid-get',
    entrypoint: './worker.js',
    compatibilityDate: '2026-11-01',
    domains: ['get.usebraid.dev'],
    workersDev: false,
    previewUrls: false,
  },
}
