#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 The Braid Authors
# Usage: test-install.sh <dist-dir>. Tests site/install.sh against GoReleaser artifacts.
set -euo pipefail
[ $# -eq 1 ] || { echo "usage: test-install.sh <dist-dir>" >&2; exit 2; }
root=$(cd "$(dirname "$0")/.." && pwd)
dist=$(cd "$1" && pwd)
inst=$root/site/install.sh
sh_bin=sh; command -v dash >/dev/null && sh_bin=dash
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
fail=0
err() { echo "test-install: $*" >&2; fail=1; }

case "$(uname -s)" in Linux) os=linux ;; Darwin) os=darwin ;; *) echo "unsupported host" >&2; exit 1 ;; esac
case "$(uname -m)" in x86_64) arch=amd64 ;; *) arch=arm64 ;; esac
archive=$(grep -oE "braid_[^ ]+_${os}_${arch}\.tar\.gz" "$dist/checksums.txt" | head -n1)
version=${archive#braid_}; version=${version%_${os}_${arch}.tar.gz}

# 1. happy path
BRAID_DOWNLOAD_BASE="file://$dist" BRAID_INSTALL_DIR="$tmp/bin" "$sh_bin" "$inst" >"$tmp/out" 2>&1 || { cat "$tmp/out" >&2; err "happy path failed"; }
[ -x "$tmp/bin/braid" ] || err "braid not installed or not executable"
v=$("$tmp/bin/braid" version 2>&1 || true)
case "$v" in "braid "*"$version"*) ;; *) err "unexpected version output: $v" ;; esac

# 5. reinstall over existing
BRAID_DOWNLOAD_BASE="file://$dist" BRAID_INSTALL_DIR="$tmp/bin" "$sh_bin" "$inst" >/dev/null 2>&1 || err "reinstall failed"

# 2. tamper
mkdir "$tmp/bad"; cp "$dist"/*.tar.gz "$dist/checksums.txt" "$tmp/bad/"
printf x >> "$tmp/bad/$archive"
if BRAID_DOWNLOAD_BASE="file://$tmp/bad" BRAID_INSTALL_DIR="$tmp/badbin" "$sh_bin" "$inst" >/dev/null 2>"$tmp/err"; then err "tampered archive was accepted"; fi
grep -q checksum "$tmp/err" || err "tamper: stderr lacks 'checksum'"
[ ! -e "$tmp/badbin/braid" ] || err "tamper: braid was installed"

# 3. unsupported platform
BRAID_OS=freebsd BRAID_INSTALL_DIR="$tmp/x" "$sh_bin" "$inst" >/dev/null 2>"$tmp/err" && err "freebsd accepted"
grep -qi unsupported "$tmp/err" || err "freebsd: no 'unsupported'"
BRAID_ARCH=riscv64 BRAID_INSTALL_DIR="$tmp/x" "$sh_bin" "$inst" >/dev/null 2>"$tmp/err" && err "riscv64 accepted"
grep -qi unsupported "$tmp/err" || err "riscv64: no 'unsupported'"

# 4. windows shell
BRAID_OS=MINGW64_NT BRAID_INSTALL_DIR="$tmp/x" "$sh_bin" "$inst" >/dev/null 2>"$tmp/err" && err "MINGW accepted"
grep -q 'daemon/#windows' "$tmp/err" || err "MINGW: no windows pointer"

# 6. lint
sh -n "$inst" || err "sh -n failed"
if command -v shellcheck >/dev/null; then shellcheck "$inst" || err "shellcheck failed"; fi

[ "$fail" = 0 ] && echo "test-install: ok"
exit "$fail"
