#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 The Braid Authors
# Self-test for check-pins.sh and install-tool.sh.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
t=$(mktemp -d); trap 'rm -rf "$t"' EXIT
sha=0123456789abcdef0123456789abcdef01234567
lock64=$(printf 'a%.0s' {1..64})
mk() { # fresh good tree in $t/tree
  rm -rf "$t/tree"; mkdir -p "$t/tree/.github/workflows" "$t/tree/scripts"
  printf 'jobs:\n  a:\n    steps:\n      - uses: actions/checkout@%s # v1\n      - uses: ./local\n      - run: echo hi\n' "$sha" > "$t/tree/.github/workflows/ci.yml"
  echo "tool 1.0 linux amd64 https://example.com/t.tar.gz $lock64" > "$t/tree/scripts/tools.lock"
}
chk() { PINS_ROOT="$t/tree" "$here/check-pins.sh" >/dev/null 2>&1; }
ok() { mk; chk || { echo "FAIL: good tree rejected"; exit 1; }; }
bad() { # desc, then a command mutating the tree
  local d=$1; shift; mk; "$@"
  if chk; then echo "FAIL: accepted $d"; exit 1; fi
}
wf() { echo "$1" >> "$t/tree/.github/workflows/ci.yml"; }
ok
bad "tag-pinned action" wf "      - uses: actions/checkout@v4"
bad "curl | sh" wf "      - run: curl -sfL https://x.example/i.sh | sh -s"
bad "tag-only service image" wf "        image: postgres:16-alpine"
bad "@latest" wf "      - run: go install x@latest"
bad "short sha lock row" sh -c "echo 'tool 1.0 linux amd64 https://example.com/t abc123' >> '$t/tree/scripts/tools.lock'"
bad "http lock url" sh -c "echo 'tool 1.0 linux arm64 http://example.com/t $lock64' >> '$t/tree/scripts/tools.lock'"

# install-tool must refuse a tampered download and leave nothing in dest.
echo payload > "$t/blob"
os=$(uname -s | tr A-Z a-z); arch=$(uname -m); case "$arch" in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; esac
echo "tool 1.0 $os $arch file://$t/blob $lock64" > "$t/fixture.lock"
if TOOLS_LOCK="$t/fixture.lock" "$here/install-tool.sh" tool "$t/dest" >/dev/null 2>&1; then echo "FAIL: tampered download installed"; exit 1; fi
[ ! -e "$t/dest/tool" ] || { echo "FAIL: binary present after mismatch"; exit 1; }
# and accepts the right checksum
if command -v sha256sum >/dev/null; then good=$(sha256sum "$t/blob" | cut -d' ' -f1); else good=$(shasum -a 256 "$t/blob" | cut -d' ' -f1); fi
echo "tool 1.0 $os $arch file://$t/blob $good" > "$t/fixture.lock"
TOOLS_LOCK="$t/fixture.lock" "$here/install-tool.sh" tool "$t/dest" >/dev/null
[ -x "$t/dest/tool" ] || { echo "FAIL: good download not installed"; exit 1; }
echo "test-pins: ok"
