#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 The Braid Authors
# Usage: check-commit-msg.sh <message-file>. Conventional Commit subject plus DCO sign-off.
set -euo pipefail
f="${1:?usage: check-commit-msg.sh <file>}"
first="$(head -n 1 "$f")"
re='^(feat|fix|chore|ci|build|docs|refactor|test|perf)(\([a-z0-9-]+\))?!?: .+'
if ! [[ $first =~ $re ]]; then
  echo "commit subject must follow Conventional Commits: $first" >&2
  exit 1
fi
if ! grep -q '^Signed-off-by: ' "$f"; then
  echo "missing Signed-off-by line (use git commit -s)" >&2
  exit 1
fi
