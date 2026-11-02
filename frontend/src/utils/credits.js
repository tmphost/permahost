// Copyright 2026 Contextual, Inc. https://permahost.io
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

// CHART_COLORS are the credit chart colours, the same in light and dark
// (agentrq's workspace stats accents).
export const CHART_COLORS = { used: '#aec477', balance: '#a8a3d9' }

// HISTORY_DAYS is how many days the credit charts cover: one renewal period.
export const HISTORY_DAYS = 30

// OPS_LABELS name what changed a balance, by credit activity ops.
const OPS_LABELS = {
  grant: 'Sign-up grant',
  renewal: 'Renewal',
  spend: 'Hosting charge',
  refund: 'Refund',
  admin_grant: 'Root grant',
  admin_deduct: 'Root deduction',
}

// opsLabel names a credit activity's ops; unknown ones show as they are.
export function opsLabel(ops) {
  return OPS_LABELS[ops] || ops
}

// formatChange signs a credit change: +250, -3.
export function formatChange(n) {
  return n > 0 ? `+${n.toLocaleString()}` : n.toLocaleString()
}

// dailyCreditSeries turns credit activities (newest first, as the API lists
// them) into one point per UTC day for the last `days` days: `used` is what
// hosting charges took net of refunds, `balance` the balance at the end of
// the day. Activities before the first day only set the opening balance.
export function dailyCreditSeries(activities, days = HISTORY_DAYS, now = Date.now()) {
  const start = Math.floor(now / DAY_MS) - days + 1
  const used = new Array(days).fill(0)
  const closing = new Array(days).fill(null)
  let balance = 0
  for (const a of [...activities].reverse()) {
    const day = Math.min(Math.floor(Date.parse(a.createdAt) / DAY_MS) - start, days - 1)
    if (day < 0) {
      balance = a.current
      continue
    }
    if (a.ops === 'spend' || a.ops === 'refund') used[day] -= a.change
    closing[day] = a.current
  }
  const series = { used: [], balance: [] }
  for (let i = 0; i < days; i++) {
    const date = new Date((start + i) * DAY_MS).toISOString().slice(0, 10)
    balance = closing[i] ?? balance
    series.used.push({ date, count: Math.max(0, used[i]) })
    series.balance.push({ date, count: balance })
  }
  return series
}
