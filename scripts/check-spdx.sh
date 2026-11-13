#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 The Braid Authors
# Fails on tracked source files lacking the SPDX header in their first 3 lines.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
fail=0
while IFS= read -r f; do
  [ -f "$f" ] || continue
  case "$f" in third_party/*) continue ;; esac
  if ! head -n 3 "$f" | grep -q 'SPDX-License-Identifier: AGPL-3.0-or-later'; then
    echo "missing SPDX header: $f"
    fail=1
  fi
done < <(git ls-files --cached --others --exclude-standard -- '*.go' '*.sql' '*.sh' '*.ts' '*.tsx' '*.css' '*.mjs')
exit "$fail"
