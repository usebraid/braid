#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 The Braid Authors
# npm audit --audit-level=high for the package in the current directory, with a short allowlist of advisories
# that have no fixed version yet. Every entry is "ID:YYYY-MM-DD" with its reason above; it stops counting on that
# date, so an exception cannot outlive its review. Fails closed on any output shape it does not understand.
set -euo pipefail
allow=(
  # picomatch ReDoS via repeated extglobs: every version affected, no fix yet (2026-11-18); dev-only via vite/vitest/knip.
  GHSA-fc5q-39xc-vjpf:2027-03-01
)
json=$(npm audit --json --audit-level=high || true)
ALLOW="${allow[*]-}" node -e '
const fail = (m) => { console.error("npm-audit-high: " + m); process.exit(1); };
let r;
try { r = JSON.parse(require("fs").readFileSync(0, "utf8")); } catch { fail("npm audit did not print JSON"); }
if (r === null || typeof r !== "object" || Array.isArray(r)) fail("unexpected npm audit output");
if (r.error) fail(r.error.summary || r.error);
const vs = r.vulnerabilities;
if (vs === null || typeof vs !== "object" || Array.isArray(vs)) fail("no vulnerabilities map in the npm audit output");
const today = new Date().toISOString().slice(0, 10);
const allow = new Set();
for (const e of process.env.ALLOW.split(" ").filter(Boolean)) {
  const [id, until] = e.split(":");
  if (until && today <= until) allow.add(id);
}
const bad = [];
let high = 0;
for (const [name, v] of Object.entries(vs)) {
  if (!["high", "critical"].includes(String(v.severity).toLowerCase())) continue;
  high++;
  if (!Array.isArray(v.via)) { bad.push(`${name}: unreadable via`); continue; }
  const ids = v.via.filter((x) => typeof x === "object" && x !== null).map((x) => (x.url || "").split("/").pop());
  const missing = v.via.filter((x) => typeof x === "string" && !Object.hasOwn(vs, x));
  // A package flagged only through other packages (via strings) is judged by those packages, which must be listed.
  if (missing.length) bad.push(`${name}: via unlisted ${missing.join(", ")}`);
  const unallowed = ids.filter((id) => !allow.has(id));
  if (unallowed.length) bad.push(`${name} (${v.severity}): ${unallowed.join(", ") || "no advisory url"}`);
}
const m = (r.metadata || {}).vulnerabilities;
if (m === null || typeof m !== "object") fail("no metadata.vulnerabilities in the npm audit output");
if ((m.high || 0) + (m.critical || 0) !== high) fail(`metadata counts ${(m.high || 0) + (m.critical || 0)} high or critical, the map ${high}`);
if (bad.length) fail("high or critical advisories:\n  " + bad.join("\n  "));
' <<<"$json"
