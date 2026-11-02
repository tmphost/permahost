<!--
  Copyright 2026 Contextual, Inc. https://permahost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
// A row of daily line charts, each in its own card.
import ChartSVG from './ChartSVG.vue'

defineProps({
  // [{ id, title, hint, color, data: [{ date, count }] }]
  charts: { type: Array, required: true },
  loading: { type: Boolean, default: false },
  // Each card is [data-test="<testPrefix>-chart-<id>"].
  testPrefix: { type: String, default: 'credits' },
})
</script>

<template>
  <div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
    <div
      v-for="chart in charts"
      :key="chart.id"
      class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl p-5"
      :data-test="`${testPrefix}-chart-${chart.id}`"
    >
      <div class="flex items-center justify-between mb-4">
        <div class="flex items-center gap-2">
          <span class="w-2 h-2 rounded-full" :style="{ background: chart.color }"></span>
          <h2 class="text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100">{{ chart.title }}</h2>
        </div>
        <span class="text-gray-400 dark:text-zinc-500 text-[9px] font-black uppercase tracking-widest">{{ chart.hint }}</span>
      </div>
      <div class="h-48 w-full relative">
        <div v-if="loading" class="absolute inset-0 flex items-center justify-center text-[10px] font-black text-gray-300 dark:text-zinc-500 animate-pulse">
          Loading...
        </div>
        <ChartSVG v-else :data="chart.data" :color="chart.color" />
      </div>
    </div>
  </div>
</template>
