<!--
SPDX-License-Identifier: AGPL-3.0-or-later
Copyright (C) 2026 The Braid Authors
-->

# Chaos end-to-end test

Proves "nothing lost, nothing duplicated" with the real server (in-process, `servertest`) and the real daemon binary (`cmd/braid`).

A generator writes six sessions (main and subagent `.jsonl` transcripts, `.meta.json` snapshots) while the test
kills the daemon with `kill -9`, stops the server for 5-20 s, deletes originals the daemon has spooled but not yet acked, and replaces
a file with new content. At the end every recorded line must be in Postgres exactly once at its offset with its hash,
every snapshot version that lived 2 s or more must be stored, and the spool must be empty.

```sh
docker run -d --rm --name braid-test-pg -e POSTGRES_PASSWORD=test -p 55432:5432 postgres:16-alpine
export BRAID_TEST_DATABASE_URL='postgres://postgres:test@localhost:55432/postgres?sslmode=disable'
BRAID_E2E=1 go test -tags e2e -timeout 20m ./e2e/ -v
```

| Variable | Effect |
|---|---|
| `BRAID_E2E=1` | required, otherwise the test skips |
| `BRAID_E2E_DURATION` | chaos phase length, default `3m` |
| `BRAID_E2E_SEED` | seed of the generated data (logged on every run) |

Harness notes:

- The daemon talks to a plaintext loopback proxy that forwards to the TLS test server (the daemon only accepts `http` for loopback, and Go on macOS ignores `SSL_CERT_FILE`). Stopping the proxy simulates a server outage.
- Originals are only deleted or replaced while the daemon holds a spool link to the current file (it "had a chance to spool"); data the daemon never saw cannot be protected.
- `.meta.json` rewrites are paused during a fault and until the daemon has caught up. A snapshot is synced as its newest content, so a version overwritten while the daemon cannot sync is not a "sync cycle" version.
- `braid run` is started with `CLAUDE_CONFIG_DIR`, `BRAID_CONFIG_DIR` and `BRAID_DATA_DIR` pointing at temp dirs and the token in a file store.
