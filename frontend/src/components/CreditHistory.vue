<!--
  Copyright 2026 Contextual, Inc. https://permahost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
// A credit history list, newest first, with a Load older button.
import { formatChange, opsLabel } from '../utils/credits'

defineProps({
  activities: { type: Array, required: true },
  // The cursor of the next page; empty on the last.
  next: { type: String, default: '' },
  loading: { type: Boolean, default: false },
  loadingOlder: { type: Boolean, default: false },
})
defineEmits(['older'])

function formatTime(at) {
  return new Date(at).toLocaleString()
}
</script>

<template>
  <section class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl" data-test="credits-history">
    <h2 class="px-4 py-3 border-b border-gray-100 dark:border-zinc-800 text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100">
      History
    </h2>
    <p v-if="!loading && activities.length === 0" class="px-4 py-6 text-xs text-gray-400 dark:text-zinc-500" data-test="credits-empty">
      No credit activity yet.
    </p>
    <ul class="divide-y divide-gray-100 dark:divide-zinc-800">
      <li v-for="a in activities" :key="a.id" class="flex items-center gap-3 px-4 py-2.5 text-xs" data-test="credits-row">
        <div class="flex-1 min-w-0">
          <p class="font-semibold text-gray-900 dark:text-zinc-100">{{ opsLabel(a.ops) }}</p>
          <p v-if="a.note" class="text-gray-600 dark:text-zinc-300 break-words" data-test="credits-note">{{ a.note }}</p>
          <p class="text-[10px] text-gray-400 dark:text-zinc-500">
            <span v-if="a.actor" class="font-semibold text-gray-600 dark:text-zinc-300" data-test="credits-actor">{{ a.actor }} · </span
            >{{ formatTime(a.createdAt) }}
          </p>
        </div>
        <span
          :class="['font-bold tabular-nums', a.change > 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-gray-900 dark:text-zinc-100']"
          data-test="credits-change"
          >{{ formatChange(a.change) }}</span
        >
        <span class="w-16 text-right tabular-nums text-gray-500 dark:text-zinc-400" data-test="credits-current">{{ a.current.toLocaleString() }}</span>
      </li>
    </ul>
    <div v-if="next" class="px-4 py-3 border-t border-gray-100 dark:border-zinc-800">
      <button
        type="button"
        class="rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-3 py-1.5 text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100 hover:border-gray-300 dark:hover:border-zinc-700 disabled:opacity-50"
        :disabled="loadingOlder"
        data-test="credits-older"
        @click="$emit('older')"
      >
        Load older
      </button>
    </div>
  </section>
</template>
