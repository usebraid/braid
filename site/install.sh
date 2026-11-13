#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 The Braid Authors
# Installs the braid CLI with a mandatory SHA-256 check.
# Env: BRAID_VERSION (x.y.z, default latest), BRAID_DOWNLOAD_BASE (https:// or file://),
#      BRAID_INSTALL_DIR (default ~/.local/bin), BRAID_OS / BRAID_ARCH (test hooks).
set -eu

die() { echo "install.sh: $*" >&2; exit 1; }

os=${BRAID_OS:-$(uname -s)}
case "$os" in
  Darwin | darwin) os=darwin ;;
  Linux | linux) os=linux ;;
  MINGW* | MSYS* | CYGWIN*) die "Windows is not supported by this script; see https://usebraid.dev/docs/daemon/#windows" ;;
  *) die "unsupported OS: $os" ;;
esac
arch=${BRAID_ARCH:-$(uname -m)}
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) die "unsupported architecture: $arch" ;;
esac

if [ -n "${BRAID_DOWNLOAD_BASE:-}" ]; then
  base=$BRAID_DOWNLOAD_BASE
elif [ -n "${BRAID_VERSION:-}" ]; then
  base="https://github.com/usebraid/braid/releases/download/v$BRAID_VERSION"
else
  base="https://github.com/usebraid/braid/releases/latest/download"
fi
case "$base" in
  https://* | file://*) proto="=https,file" ;;
  *) die "BRAID_DOWNLOAD_BASE must start with https:// or file://" ;;
esac
dir=${BRAID_INSTALL_DIR:-$HOME/.local/bin}

if command -v sha256sum >/dev/null 2>&1; then
  sha() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
  sha() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
  die "neither sha256sum nor shasum found"
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fetch() { curl -fsSL --proto "$proto" "$1" -o "$2" || die "download failed: $1"; }

fetch "$base/checksums.txt" "$tmp/checksums.txt"
line=$(awk -v re="^braid_[^ ]+_${os}_${arch}\\\\.tar\\\\.gz\$" '$2 ~ re' "$tmp/checksums.txt")
[ -n "$line" ] || die "no archive for $os/$arch in checksums.txt"
[ "$(printf '%s\n' "$line" | wc -l | tr -d ' ')" = 1 ] || die "several archives for $os/$arch in checksums.txt"
want=${line%% *}
name=${line##* }
name=${name#\*}

fetch "$base/$name" "$tmp/$name"
got=$(sha "$tmp/$name")
if [ "$got" != "$want" ]; then
  echo "install.sh: checksum mismatch for $name" >&2
  echo "  expected: $want" >&2
  echo "  got:      $got" >&2
  exit 1
fi

tar -xzf "$tmp/$name" -C "$tmp" braid
mkdir -p "$dir"
cp "$tmp/braid" "$dir/.braid.$$"
chmod 0755 "$dir/.braid.$$"
mv -f "$dir/.braid.$$" "$dir/braid"

echo "Installed $dir/braid"
"$dir/braid" version
case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "Add $dir to your PATH, e.g. export PATH=\"$dir:\$PATH\"" ;;
esac
cat <<MSG

Next:
  braid login <server-url>
  braid install

Signatures can be verified as shown at https://usebraid.dev/docs/
MSG
