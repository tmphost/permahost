<!--
  Copyright 2026 Contextual, Inc. https://permahost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
// A summary card: a small label, an optional colour dot matching a chart,
// a big number and an optional hint (the hint slot) under it.
defineProps({
  label: { type: String, required: true },
  value: { type: [Number, String], default: 0 },
  // The dot's colour; no dot without it.
  color: { type: String, default: '' },
  // The value's data-test.
  test: { type: String, required: true },
})
</script>

<template>
  <div class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl p-4 flex flex-col gap-1.5 min-w-0">
    <div class="flex items-center gap-1.5">
      <span v-if="color" class="w-1.5 h-1.5 rounded-full" :style="{ background: color }"></span>
      <span class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 truncate">{{ label }}</span>
    </div>
    <span class="text-2xl font-black text-gray-900 dark:text-zinc-100 tabular-nums leading-none truncate" :data-test="test">{{ value }}</span>
    <slot />
  </div>
</template>
