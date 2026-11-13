// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

const CATEGORY_LABEL: Record<string, string> = {
  usage_limit: 'Usage limit',
  rate_limit: 'Rate limit',
  server_error: 'Server error',
  network: 'Network',
  refusal: 'Refusal',
  tool_error: 'Failed tool call',
  other_api_error: 'Other API error',
}

const LIMIT_LABEL: Record<string, string> = {
  five_hour: '5-hour limit',
  seven_day: '7-day limit',
  seven_day_opus: '7-day Opus limit',
  seven_day_sonnet: '7-day Sonnet limit',
}

export const categoryLabel = (k: string) => CATEGORY_LABEL[k] ?? k
export const limitLabel = (k: string) => LIMIT_LABEL[k] ?? k

// Pill tone: limits and refusals warn, errors are bad.
export const categoryTone = (k: string): 'warn' | 'bad' =>
  k === 'usage_limit' || k === 'rate_limit' || k === 'refusal' ? 'warn' : 'bad'
