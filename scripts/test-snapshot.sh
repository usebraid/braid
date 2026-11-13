#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 The Braid Authors
# Usage: test-snapshot.sh <dist-dir>. Asserts a GoReleaser snapshot/release dist has the expected artifacts and runs.
set -euo pipefail
[ $# -eq 1 ] || { echo "usage: test-snapshot.sh <dist-dir>" >&2; exit 2; }
dist=$(cd "$1" && pwd)
fail=0
err() { echo "test-snapshot: $*" >&2; fail=1; }
count() { find "$dist" -maxdepth 1 -name "$1" | wc -l | tr -d ' '; }
expect() { [ "$(count "$1")" = "$2" ] || err "$1: want $2, got $(count "$1")"; }

expect 'braid_*.tar.gz' 4
expect 'braid_*.zip' 2
expect 'braid-server_*_linux_*.tar.gz' 2
expect 'braid_*.sbom.json' 6
expect 'braid-server_*.sbom.json' 2
[ -f "$dist/checksums.txt" ] || err "checksums.txt missing"
[ -f "$dist/braid.rb" ] || [ -f "$dist/homebrew/braid.rb" ] || err "braid.rb missing"
[ "$fail" = 0 ] || exit 1

(cd "$dist" && if command -v sha256sum >/dev/null; then sha256sum -c checksums.txt; else shasum -a 256 -c checksums.txt; fi >/dev/null) || err "checksums.txt does not verify"

case "$(uname -s)" in Linux) os=linux ;; Darwin) os=darwin ;; *) err "unsupported host"; exit 1 ;; esac
case "$(uname -m)" in x86_64) arch=amd64 ;; arm64|aarch64) arch=arm64 ;; esac
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
run() { # name, archive os (server only exists for linux)
  local a; a=$(find "$dist" -maxdepth 1 -name "$1_*_${2}_${arch}.tar.gz" | head -n1)
  [ -n "$a" ] || { err "no $1 archive for $2/$arch"; return; }
  mkdir "$tmp/$1"; tar -xzf "$a" -C "$tmp/$1"
  local want; want=$(basename "$a" | sed -E "s/^$1_(.*)_${2}_${arch}\.tar\.gz$/\1/")
  "$tmp/$1/$1" version | grep -q "^$1 $want " || err "$1 version output lacks $want: $("$tmp/$1/$1" version)"
}
run braid "$os"
[ "$os" = linux ] && run braid-server linux
exit "$fail"
