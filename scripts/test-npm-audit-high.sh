#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 The Braid Authors
# Self-test for npm-audit-high.sh with a fake npm: it must fail closed on output shapes it does not understand.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
t=$(mktemp -d); trap 'rm -rf "$t"' EXIT
mkdir "$t/bin"
printf '#!/bin/sh\ncat "$FAKE_JSON"\nexit 1\n' > "$t/bin/npm"; chmod +x "$t/bin/npm"
run() { # expected, name, json
  printf '%s' "$3" > "$t/j"
  local got=pass
  FAKE_JSON="$t/j" PATH="$t/bin:$PATH" "$here/npm-audit-high.sh" >/dev/null 2>&1 || got=fail
  [ "$got" = "$1" ] || { echo "FAIL: $2 (wanted $1)"; exit 1; }
}
url() { echo "{\"url\":\"https://github.com/advisories/$1\"}"; }
meta() { echo "\"metadata\":{\"vulnerabilities\":{\"high\":$1,\"critical\":$2}}"; }
OK=$(url GHSA-fc5q-39xc-vjpf); OTHER=$(url GHSA-aaaa-bbbb-cccc)
run pass "only the allowlisted advisory" "{\"vulnerabilities\":{\"p\":{\"severity\":\"high\",\"via\":[$OK]}},$(meta 1 0)}"
run pass "transitive through a listed package" "{\"vulnerabilities\":{\"p\":{\"severity\":\"high\",\"via\":[$OK]},\"v\":{\"severity\":\"high\",\"via\":[\"p\"]}},$(meta 2 0)}"
run pass "nothing found" "{\"vulnerabilities\":{},$(meta 0 0)}"
run fail "other high advisory" "{\"vulnerabilities\":{\"y\":{\"severity\":\"high\",\"via\":[$OTHER]}},$(meta 1 0)}"
run fail "critical" "{\"vulnerabilities\":{\"y\":{\"severity\":\"critical\",\"via\":[$OTHER]}},$(meta 0 1)}"
run fail "upper-case severity" "{\"vulnerabilities\":{\"y\":{\"severity\":\"HIGH\",\"via\":[$OTHER]}},$(meta 1 0)}"
run fail "advisory without url" "{\"vulnerabilities\":{\"y\":{\"severity\":\"high\",\"via\":[{\"source\":9}]}},$(meta 1 0)}"
run fail "via names a missing package" "{\"vulnerabilities\":{\"v\":{\"severity\":\"high\",\"via\":[\"ghost\"]}},$(meta 1 0)}"
run fail "npm v6 shape" "{\"advisories\":{\"1\":{\"severity\":\"high\"}},$(meta 1 0)}"
run fail "metadata counts a high the map lacks" "{\"vulnerabilities\":{},$(meta 1 0)}"
run fail "no metadata" "{\"vulnerabilities\":{}}"
run fail "json array" "[]"
run fail "json null" "null"
run fail "not json" "npm ERR! network"
run fail "empty output" ""
run fail "npm error" '{"error":{"summary":"registry down"}}'
run fail "via not an array" "{\"vulnerabilities\":{\"y\":{\"severity\":\"high\",\"via\":\"z\"}},$(meta 1 0)}"
echo "test-npm-audit-high: ok"
