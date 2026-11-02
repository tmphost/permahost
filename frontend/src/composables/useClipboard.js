// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { useToasts } from './useToasts'

// useClipboard copies text and reports the outcome with a toast.
export function useClipboard() {
  const { notifyError, notifySuccess } = useToasts()

  async function copy(text, message = 'Copied to clipboard') {
    try {
      await navigator.clipboard.writeText(text)
      notifySuccess(message)
      return true
    } catch {
      notifyError('Could not copy to the clipboard')
      return false
    }
  }

  return { copy }
}
