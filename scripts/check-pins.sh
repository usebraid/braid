#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 The Braid Authors
# Fails on unpinned workflow actions, curl|sh, @latest, and malformed tools.lock rows. PINS_ROOT overrides the repo root.
set -euo pipefail
root=${PINS_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}
fail=0
err() { echo "check-pins: $*" >&2; fail=1; }
shopt -s nullglob
for f in "$root"/.github/workflows/*.yml; do
  while IFS= read -r ref; do
    case "$ref" in ./*) continue ;; esac
    [[ "$ref" =~ @[0-9a-f]{40}$ ]] || err "$f: action not pinned to a 40-hex sha: $ref"
  done < <(sed -nE 's/^[[:space:]-]*uses:[[:space:]]*([^[:space:]#]+).*/\1/p' "$f")
  grep -nE 'curl[^|]*\|[[:space:]]*(sudo[[:space:]]+)?(ba)?sh' "$f" >&2 && err "$f: curl piped to a shell"
  grep -n '@latest' "$f" >&2 && err "$f: @latest"
done
if [ -f "$root/scripts/tools.lock" ]; then
  while read -r n v o a url sum rest; do
    case "$n" in ''|'#'*) continue ;; esac
    [[ "$sum" =~ ^[0-9a-f]{64}$ && -z "$rest" ]] || err "tools.lock: bad row/sha256 for $n $o/$a"
    [[ "$url" == https://* ]] || err "tools.lock: non-https url for $n $o/$a"
  done < "$root/scripts/tools.lock"
fi
exit "$fail"
