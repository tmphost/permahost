// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

const HOUR_MIN = 60
const DAY_MIN = 24 * HOUR_MIN

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

// storageBlocks is the number of started storage blocks bytes take up.
export function storageBlocks(bytes, blockBytes) {
  return Math.ceil(Math.max(0, bytes) / blockBytes)
}

// uploadCost estimates the credits an upload spends now: the folder price
// for a new folder, plus each storage block the account's stored total grows
// into. Both are charged again every month.
export function uploadCost({ newFolder, storedBytes, addedBytes, pricing }) {
  const blocks =
    storageBlocks(storedBytes + addedBytes, pricing.storageBlockBytes) - storageBlocks(storedBytes, pricing.storageBlockBytes)
  return (newFolder ? pricing.folderCredits : 0) + Math.max(0, blocks) * pricing.storageCredits
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
