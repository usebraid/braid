#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 The Braid Authors
# Usage: install-tool.sh <name> <dest-dir>. Installs a tool pinned in scripts/tools.lock (TOOLS_LOCK overrides).
set -euo pipefail
[ $# -eq 2 ] || { echo "usage: install-tool.sh <name> <dest-dir>" >&2; exit 2; }
name=$1 dest=$2
lock=${TOOLS_LOCK:-$(cd "$(dirname "$0")" && pwd)/tools.lock}
case "$(uname -s)" in Linux) os=linux ;; Darwin) os=darwin ;; *) echo "unsupported os $(uname -s)" >&2; exit 1 ;; esac
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; arm64|aarch64) arch=arm64 ;; *) echo "unsupported arch $(uname -m)" >&2; exit 1 ;; esac

read -r url want < <(awk -v n="$name" -v o="$os" -v a="$arch" '$1==n && $3==o && $4==a {print $5, $6; exit}' "$lock") || true
[ -n "${url:-}" ] || { echo "install-tool: no row for $name $os/$arch in $lock" >&2; exit 1; }

tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
curl -fsSL --retry 3 "$url" -o "$tmp/dl"
if command -v sha256sum >/dev/null; then got=$(sha256sum "$tmp/dl" | cut -d' ' -f1); else got=$(shasum -a 256 "$tmp/dl" | cut -d' ' -f1); fi
if [ "$got" != "$want" ]; then
  echo "install-tool: sha256 mismatch for $name (want $want, got $got)" >&2
  exit 1
fi

mkdir -p "$dest"
case "$url" in
  *.tar.gz) tar -xzf "$tmp/dl" -C "$dest" "$name" ;;
  *) cp "$tmp/dl" "$dest/$name" ;;
esac
chmod +x "$dest/$name"
