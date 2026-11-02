// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { ref } from 'vue'

const DURATION = 4000
const ERROR_DURATION = 8000
const MAX_VISIBLE = 4

const toasts = ref([])
let nextId = 1

function removeToast(id) {
  toasts.value = toasts.value.filter((t) => t.id !== id)
}

function addToast(message, type = 'info', title = null) {
  const id = nextId++
  const duration = type === 'error' ? ERROR_DURATION : DURATION
  toasts.value = [...toasts.value, { id, message, type, title }].slice(-MAX_VISIBLE)
  setTimeout(() => removeToast(id), duration)
  return id
}

export function useToasts() {
  return {
    toasts,
    addToast,
    removeToast,
    notifyError: (message, title = null) => addToast(message, 'error', title),
    notifySuccess: (message, title = null) => addToast(message, 'success', title),
    notifyInfo: (message, title = null) => addToast(message, 'info', title),
  }
}
