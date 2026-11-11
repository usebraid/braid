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
golangci-lint run
govulncheck ./...
scripts/check-spdx.sh
gitleaks detect --no-banner

if [ -z "${BRAID_TEST_DATABASE_URL:-}" ]; then
  if ! docker ps --format '{{.Names}}' | grep -qx braid-test-pg; then
    docker run -d --rm --name braid-test-pg -e POSTGRES_PASSWORD=test -p 55432:5432 postgres:16-alpine >/dev/null
  fi
  until docker exec braid-test-pg pg_isready -U postgres >/dev/null 2>&1; do sleep 1; done
  export BRAID_TEST_DATABASE_URL='postgres://postgres:test@localhost:55432/postgres?sslmode=disable'
fi
go test -race ./...
