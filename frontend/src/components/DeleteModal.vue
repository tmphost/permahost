<!--
  Copyright 2026 Contextual, Inc. https://tmphost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
// DeleteModal confirms a destructive action. The parent owns the request:
// it sets busy while working and closes the modal by clearing open.
const props = defineProps({
  open: { type: Boolean, default: false },
  title: { type: String, default: 'Delete?' },
  message: { type: String, default: '' },
  confirmLabel: { type: String, default: 'Delete' },
  busy: { type: Boolean, default: false },
})
const emit = defineEmits(['confirm', 'cancel'])

function cancel() {
  if (!props.busy) emit('cancel')
}
</script>

<template>
  <div
    v-if="open"
    class="fixed inset-0 z-[150] flex items-center justify-center bg-black/40 backdrop-blur-sm px-4"
    data-test="delete-modal"
    @click.self="cancel"
    @keydown.esc="cancel"
  >
    <div
      role="alertdialog"
      aria-modal="true"
      aria-labelledby="delete-modal-title"
      class="w-full max-w-sm bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-sm p-5"
    >
      <h2 id="delete-modal-title" class="text-sm font-bold text-gray-900 dark:text-zinc-100">{{ title }}</h2>
      <p v-if="message" class="mt-1.5 text-xs text-gray-500 dark:text-zinc-400 break-words" data-test="delete-modal-message">{{ message }}</p>
      <div class="mt-5 flex justify-end gap-2">
        <button
          type="button"
          :disabled="busy"
          class="rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100 hover:border-gray-300 dark:hover:border-zinc-600 disabled:opacity-50 transition-colors"
          data-test="delete-modal-cancel"
          @click="cancel"
        >
          Cancel
        </button>
        <button
          type="button"
          :disabled="busy"
          class="rounded-lg bg-rose-600 px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-white hover:bg-rose-700 disabled:opacity-50 transition-colors"
          data-test="delete-modal-confirm"
          @click="emit('confirm')"
        >
          {{ busy ? 'Deleting…' : confirmLabel }}
        </button>
      </div>
    </div>
  </div>
</template>
