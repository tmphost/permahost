<!--
  Copyright 2026 Contextual, Inc. https://tmphost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
import { useToasts } from '../composables/useToasts'

const { toasts, removeToast } = useToasts()

const ACCENT = {
  error: 'bg-rose-500',
  success: 'bg-emerald-500',
  info: 'bg-gray-400',
}
</script>

<template>
  <div class="fixed bottom-14 md:bottom-4 right-4 left-4 md:left-auto z-[200] flex flex-col gap-2 md:w-80 pointer-events-none">
    <div
      v-for="toast in toasts"
      :key="toast.id"
      role="status"
      class="pointer-events-auto flex items-start gap-3 bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl p-3 shadow-sm"
      :data-test="`toast-${toast.type}`"
    >
      <span :class="['mt-1.5 w-1.5 h-1.5 rounded-full shrink-0', ACCENT[toast.type] || ACCENT.info]"></span>
      <div class="grow min-w-0">
        <p v-if="toast.title" class="text-xs font-bold text-gray-900 dark:text-zinc-100">{{ toast.title }}</p>
        <p class="text-xs text-gray-500 dark:text-zinc-400 break-words">{{ toast.message }}</p>
      </div>
      <button
        type="button"
        class="text-gray-400 dark:text-zinc-500 hover:text-gray-900 dark:hover:text-zinc-100 transition-colors"
        aria-label="Dismiss"
        @click="removeToast(toast.id)"
      >
        <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="round" stroke-linejoin="round" d="M6 18 18 6M6 6l12 12" />
        </svg>
      </button>
    </div>
  </div>
</template>
