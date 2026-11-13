#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 The Braid Authors
# Checks README, CREDITS, CONTRIBUTING, the install one-liner and the release checklist for consistency.
set -euo pipefail
cd "$(dirname "$0")/.."
fail=0
err() { echo "check-readme: $*" >&2; fail=1; }

oneliner='curl -fsSL https://get.usebraid.dev | sh'
for f in README.md site/index.html site/docs/quick-start.md; do
  grep -qF "$oneliner" "$f" || err "$f: missing the install one-liner"
done
for h in Install "Special thanks" "Notable dependencies"; do
  grep -qE "^##+ $h$" README.md || err "README.md: missing heading '$h'"
done
for s in ponytail agents-observe; do
  for f in README.md CREDITS.md; do grep -q "$s" "$f" || err "$f: missing $s"; done
done

# Every package or module named in backticks under "Notable dependencies" exists in a manifest.
deps=$(awk '/^## Notable dependencies$/ {f=1; next} /^## / {f=0} f' README.md | grep -o '`[^`]*`' | tr -d '`' | grep -E '^@|/|^marked$' || true)
[ -n "$deps" ] || err "README.md: no dependencies listed"
for d in $deps; do
  grep -qF "\"$d\"" web/package.json site/package.json || grep -qE "^[[:space:]]*$d v" go.mod || err "README.md: $d not found in web/package.json, site/package.json or go.mod"
done

# Relative links in the root Markdown files resolve.
for f in README.md CREDITS.md CONTRIBUTING.md; do
  for l in $(grep -o '\]([^)]*)' "$f" | sed 's/^](//; s/)$//' | grep -vE '^(https?:|mailto:|#)' || true); do
    [ -e "${l%%#*}" ] || err "$f: broken relative link $l"
  done
done

# Release checklist
c=docs/RELEASE_CHECKLIST.md
if [ -f "$c" ]; then
  for s in "cosign verify-blob" "imagetools inspect" "brew install" "install.sh" "band/RELEASE_CHECKLIST.md"; do
    grep -qF "$s" "$c" || err "$c: missing '$s'"
  done
  grep -qF "docs/RELEASE_CHECKLIST.md" CONTRIBUTING.md || err "CONTRIBUTING.md: does not link the release checklist"
else
  err "$c missing"
fi
exit "$fail"
