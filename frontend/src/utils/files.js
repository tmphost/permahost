// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

const MINUTE_MS = 60 * 1000
const HOUR_MIN = 60
const DAY_MIN = 24 * HOUR_MIN

// TTL_CHOICES are the lifetimes offered on upload, capped by the server limit.
export const TTL_CHOICES = [30, 60, 120, 360, 720, 1440, 10080]

export function ttlOptions(maxTtlMinutes) {
  return TTL_CHOICES.filter((m) => m <= maxTtlMinutes)
}

// formatMinutes renders a duration such as "30 min", "6 h", "7 days".
export function formatMinutes(minutes) {
  if (minutes >= DAY_MIN && minutes % DAY_MIN === 0) {
    const days = minutes / DAY_MIN
    return days === 1 ? '1 day' : `${days} days`
  }
  if (minutes >= HOUR_MIN && minutes % HOUR_MIN === 0) return `${minutes / HOUR_MIN} h`
  if (minutes > HOUR_MIN) return `${Math.floor(minutes / HOUR_MIN)} h ${minutes % HOUR_MIN} min`
  return `${minutes} min`
}

// uploadCost is the credits charged for files of the given sizes: one per
// started billing unit of each file's size per started billing block, and
// at least one of each.
export function uploadCost(sizes, ttlMinutes, billingBlockMinutes, billingUnitBytes) {
  const blocks = Math.max(1, Math.ceil(ttlMinutes / billingBlockMinutes))
  return sizes.reduce((sum, size) => sum + Math.max(1, Math.ceil(size / billingUnitBytes)) * blocks, 0)
}

export function formatBytes(bytes) {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB']
  let value = bytes / 1024
  let i = 0
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024
    i++
  }
  return `${value.toFixed(1)} ${units[i]}`
}

// expiresIn is a coarse countdown such as "45m", "5h 12m" or "3d 4h".
export function expiresIn(expiresAt, now = Date.now()) {
  const at = Date.parse(expiresAt)
  if (Number.isNaN(at)) return ''
  const minutes = Math.ceil((at - now) / MINUTE_MS)
  if (minutes <= 0) return 'Expired'
  if (minutes < HOUR_MIN) return `${minutes}m`
  if (minutes < DAY_MIN) return `${Math.floor(minutes / HOUR_MIN)}h ${minutes % HOUR_MIN}m`
  return `${Math.floor(minutes / DAY_MIN)}d ${Math.floor((minutes % DAY_MIN) / HOUR_MIN)}h`
}

// relativePath is where a picked file lands in its folder: the path inside a
// chosen directory, or just its name.
export function relativePath(file) {
  return file.webkitRelativePath || file.name
}

// fileItems pairs picked files with their relativePath.
export function fileItems(fileList) {
  return [...fileList].map((file) => ({ path: relativePath(file), file }))
}

// isFileDrag tells a drag of files or folders from one of text or links.
export function isFileDrag(event) {
  return [...(event.dataTransfer?.types ?? [])].includes('Files')
}

// droppedFiles lists a drop as { path, file } items, walking dropped folders
// so their files keep paths relative to the drop (photos/2026/a.jpg). It reads
// the entries before its first await: the browser empties the DataTransfer
// once the drop handler returns.
export async function droppedFiles(dataTransfer) {
  const entries = [...(dataTransfer.items ?? [])].map((item) => item.webkitGetAsEntry?.()).filter(Boolean)
  if (!entries.length) return fileItems(dataTransfer.files)
  return (await Promise.all(entries.map(readEntry))).flat()
}

async function readEntry(entry) {
  if (entry.isFile) {
    const file = await new Promise((resolve, reject) => entry.file(resolve, reject))
    return [{ path: entry.fullPath.replace(/^\/+/, ''), file }]
  }
  const reader = entry.createReader()
  const children = []
  // readEntries hands back a batch at a time (100 in Chrome); empty means done.
  for (;;) {
    const batch = await new Promise((resolve, reject) => reader.readEntries(resolve, reject))
    if (!batch.length) break
    children.push(...batch)
  }
  return (await Promise.all(children.map(readEntry))).flat()
}
