// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

const DAY_MS = 24 * 60 * 60 * 1000

// creditsPercent is the share of the monthly grant still available (0-100).
export function creditsPercent(user) {
  if (!user || !user.creditsLimit) return 0
  const pct = Math.round((user.credits / user.creditsLimit) * 100)
  return Math.min(100, Math.max(0, pct))
}

// renewsIn describes when the operation credits next reset.
export function renewsIn(renewAt, now = Date.now()) {
  const at = Date.parse(renewAt)
  if (Number.isNaN(at)) return ''
  const days = Math.ceil((at - now) / DAY_MS)
  if (days <= 0) return 'Renews today'
  if (days === 1) return 'Renews tomorrow'
  return `Renews in ${days} days`
}
