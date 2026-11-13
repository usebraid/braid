// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import { defineConfig } from '@playwright/test'

// Needs BRAID_TEST_DATABASE_URL (a Postgres superuser URL) and a built web/dist.
export default defineConfig({
  testDir: 'e2e',
  workers: 1,
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
  use: { baseURL: 'http://localhost:4173', trace: 'retain-on-failure' },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }],
  webServer: {
    command:
      'cd .. && go test -c -tags webe2e -o web/.e2e/harness ./e2e/web && web/.e2e/harness -test.run ^TestServe$ -test.timeout 0',
    url: 'http://localhost:4173/healthz',
    reuseExistingServer: !process.env.CI,
    timeout: 180_000,
    stdout: 'pipe',
  },
})
