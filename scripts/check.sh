#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 The Braid Authors
# Runs all static checks and tests. Run before every commit.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
export PATH="$(go env GOPATH)/bin:$PATH"

out="$(gofmt -l .)"
if [ -n "$out" ]; then echo "gofmt needed:"; echo "$out"; exit 1; fi
go vet ./...
go vet -tags e2e ./e2e/
go vet -tags webe2e ./e2e/web/
golangci-lint run
govulncheck ./...
scripts/check-spdx.sh
scripts/test-pins.sh
scripts/check-pins.sh
scripts/check-readme.sh
gitleaks detect --no-banner
if command -v trivy >/dev/null; then trivy config --exit-code 1 --severity HIGH,CRITICAL . ; fi

if [ -z "${BRAID_TEST_DATABASE_URL:-}" ]; then
  if ! docker ps --format '{{.Names}}' | grep -qx braid-test-pg; then
    docker run -d --rm --name braid-test-pg -e POSTGRES_PASSWORD=test -p 55432:5432 postgres:16-alpine >/dev/null
  fi
  until docker exec braid-test-pg pg_isready -U postgres >/dev/null 2>&1; do sleep 1; done
  export BRAID_TEST_DATABASE_URL='postgres://postgres:test@localhost:55432/postgres?sslmode=disable'
fi
go test -race ./...
if [ "${BRAID_E2E:-}" = 1 ]; then go test -tags e2e -count=1 -timeout 20m ./e2e/; fi

if [ -f web/package.json ]; then
  (cd web && npm ci --ignore-scripts && npm run check && npm run gen:api:check && npm audit --omit=dev --audit-level=low && npm audit --audit-level=high && npm run build)
  if [ "${BRAID_WEB_E2E:-}" = 1 ]; then (cd web && npx playwright install chromium && npx playwright test); fi
fi

if [ -f band/package.json ]; then
  (cd band && npm ci --ignore-scripts && npm run check && npm audit --omit=dev --audit-level=low && npm audit --audit-level=high)
fi
