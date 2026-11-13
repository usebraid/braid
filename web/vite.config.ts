// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

import react from '@vitejs/plugin-react'
import license from 'rollup-plugin-license'
import { configDefaults, defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [
    react(),
    license({
      thirdParty: {
        allow: '(MIT OR ISC OR BSD-2-Clause OR BSD-3-Clause OR Apache-2.0 OR 0BSD)',
        output: 'dist/third-party-licenses.txt',
      },
    }),
  ],
  build: {
    outDir: 'dist',
    assetsInlineLimit: 0,
    modulePreload: { polyfill: false },
    sourcemap: false,
    target: 'es2023',
  },
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/auth': 'http://localhost:8080',
      '/v1': 'http://localhost:8080',
    },
  },
  test: {
    environment: 'jsdom',
    exclude: [...configDefaults.exclude, 'e2e/**'], // Playwright specs
    setupFiles: ['src/test/setup.ts'],
  },
})
