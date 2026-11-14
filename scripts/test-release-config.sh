#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 The Braid Authors
# Asserts release-please-config.json parses and every extra-files target and jsonpath key exists.
set -euo pipefail
cd "$(dirname "$0")/.."
node -e '
const fs = require("fs")
const cfg = JSON.parse(fs.readFileSync("release-please-config.json", "utf8"))
const files = cfg.packages["."]["extra-files"]
if (!files || !files.length) throw new Error("no extra-files")
for (const f of files) {
  if (f.type !== "json") { fs.accessSync(f.path); continue }
  const doc = JSON.parse(fs.readFileSync(f.path, "utf8"))
  // only the two jsonpath shapes we use: $.a.b and $.packages[""].version
  const keys = [...f.jsonpath.matchAll(/\.([A-Za-z_]+)|\[\x27([^\x27]*)\x27\]/g)].map((m) => m[1] ?? m[2])
  let cur = doc
  for (const k of keys) {
    if (cur === null || typeof cur !== "object" || !(k in cur)) throw new Error(`${f.path}: ${f.jsonpath} missing at "${k}"`)
    cur = cur[k]
  }
}
console.log("release config ok")
'
