#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 The Braid Authors
# npm audit --audit-level=high for the package in the current directory, with a short allowlist of advisories
# that have no fixed version yet. Every entry names its reason; drop it once a fix exists.
set -euo pipefail
allow=(
  # picomatch ReDoS via repeated extglobs: every version affected, no fix yet (2026-11-18); dev-only via vite/vitest/knip.
  GHSA-fc5q-39xc-vjpf
)
json=$(npm audit --json --audit-level=high || true)
ALLOW="${allow[*]}" node -e '
const r = JSON.parse(require("fs").readFileSync(0, "utf8"));
if (r.error) { console.error("npm-audit-high:", r.error.summary || r.error); process.exit(1); }
const allow = new Set(process.env.ALLOW.split(" "));
const bad = [];
for (const [name, v] of Object.entries(r.vulnerabilities || {})) {
  if (!["high", "critical"].includes(v.severity)) continue;
  const ids = v.via.filter((x) => typeof x === "object").map((x) => (x.url || "").split("/").pop());
  // A package flagged only through other packages (via strings) is judged by those packages.
  if (ids.length === 0 || ids.some((id) => !allow.has(id))) {
    if (ids.length > 0) bad.push(`${name} (${v.severity}): ${ids.filter((id) => !allow.has(id)).join(", ")}`);
  }
}
if (bad.length) { console.error("npm-audit-high: high or critical advisories:\n  " + bad.join("\n  ")); process.exit(1); }
' <<<"$json"
