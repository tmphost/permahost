// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Fakes of the FileSystemEntry API a browser hands a drop handler, for tests.

export function fileEntry(fullPath, size = 10, error = null) {
  const name = fullPath.split('/').pop()
  return {
    isFile: true,
    isDirectory: false,
    fullPath,
    file: (resolve, reject) => (error ? reject(error) : resolve({ name, size, webkitRelativePath: '' })),
  }
}

// dirEntry serves its children in batches of batchSize, like Chrome's 100.
export function dirEntry(fullPath, children, batchSize = 100) {
  return {
    isFile: false,
    isDirectory: true,
    fullPath,
    createReader() {
      let offset = 0
      return {
        readEntries(resolve) {
          const batch = children.slice(offset, offset + batchSize)
          offset += batch.length
          resolve(batch)
        },
      }
    },
  }
}

// dataTransfer wraps entries as the DataTransfer of a drop of files.
export function dataTransfer(entries) {
  return {
    types: ['Files'],
    files: [],
    items: entries.map((entry) => ({ kind: 'file', webkitGetAsEntry: () => entry })),
  }
}
