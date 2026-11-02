<!--
  Copyright 2026 Contextual, Inc. https://permahost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
// A smooth line chart of daily counts, ported from agentrq's workspace stats
// (frontend/src/components/ChartSVG.vue there).
import { computed, ref } from 'vue'

const props = defineProps({
  // [{ date: 'YYYY-MM-DD', count: N }], oldest first.
  data: { type: Array, default: () => [] },
  color: { type: String, default: '#27272a' },
})

const hoveredPoint = ref(null)

const maxValue = computed(() => Math.max(1, ...props.data.map((d) => d.count)))
// A balance can go below zero; the axis then starts there instead of at 0.
const minValue = computed(() => Math.min(0, ...props.data.map((d) => d.count)))

// x spans 5..95 and y 82..18 of the 100x100 viewBox.
const points = computed(() => {
  const len = props.data.length
  const range = maxValue.value - minValue.value
  return props.data.map((d, i) => ({
    x: len === 1 ? 50 : (i / (len - 1)) * 90 + 5,
    y: 82 - ((d.count - minValue.value) / range) * 64,
    count: d.count,
    date: formatDate(d.date),
  }))
})

// computeControlPoints solves for the cubic Bezier control points of a
// natural spline through K (at least 3 values).
function computeControlPoints(K) {
  const m = K.length - 1
  const p1 = new Array(m)
  const p2 = new Array(m)
  const a = new Array(m)
  const b = new Array(m)
  const c = new Array(m)
  const r = new Array(m)
  a[0] = 0
  b[0] = 2
  c[0] = 1
  r[0] = K[0] + 2 * K[1]
  for (let i = 1; i < m - 1; i++) {
    a[i] = 1
    b[i] = 4
    c[i] = 1
    r[i] = 4 * K[i] + 2 * K[i + 1]
  }
  a[m - 1] = 2
  b[m - 1] = 7
  c[m - 1] = 0
  r[m - 1] = 8 * K[m - 1] + K[m]
  for (let i = 1; i < m; i++) {
    const w = a[i] / b[i - 1]
    b[i] -= w * c[i - 1]
    r[i] -= w * r[i - 1]
  }
  p1[m - 1] = r[m - 1] / b[m - 1]
  for (let i = m - 2; i >= 0; --i) p1[i] = (r[i] - c[i] * p1[i + 1]) / b[i]
  for (let i = 0; i < m - 1; i++) p2[i] = 2 * K[i + 1] - p1[i + 1]
  p2[m - 1] = (K[m] + p1[m - 1]) / 2
  return { p1, p2 }
}

const clampY = (y) => Math.max(10, Math.min(85, y)).toFixed(2)

const linePath = computed(() => {
  const pts = points.value
  if (pts.length === 0) return ''
  if (pts.length === 1) {
    const p = pts[0]
    return `M ${(p.x - 5).toFixed(2)} ${p.y.toFixed(2)} L ${(p.x + 5).toFixed(2)} ${p.y.toFixed(2)}`
  }
  if (pts.length === 2) {
    return `M ${pts[0].x.toFixed(2)} ${pts[0].y.toFixed(2)} L ${pts[1].x.toFixed(2)} ${pts[1].y.toFixed(2)}`
  }
  const xCtrl = computeControlPoints(pts.map((p) => p.x))
  const yCtrl = computeControlPoints(pts.map((p) => p.y))
  let path = `M ${pts[0].x.toFixed(2)} ${pts[0].y.toFixed(2)}`
  for (let i = 0; i < pts.length - 1; i++) {
    path += ` C ${xCtrl.p1[i].toFixed(2)} ${clampY(yCtrl.p1[i])}, ${xCtrl.p2[i].toFixed(2)} ${clampY(yCtrl.p2[i])}, ${pts[i + 1].x.toFixed(2)} ${pts[i + 1].y.toFixed(2)}`
  }
  return path
})

function handleMouseMove(e) {
  if (points.value.length === 0) return
  const rect = e.currentTarget.getBoundingClientRect()
  const mouseX = ((e.clientX - rect.left) / (rect.width || 1)) * 100
  hoveredPoint.value = points.value.reduce((best, p) => (Math.abs(mouseX - p.x) < Math.abs(mouseX - best.x) ? p : best))
}

// formatDate shows YYYY-MM-DD as MM-DD.
function formatDate(date) {
  return date.slice(5)
}

// About six evenly spaced labels, always ending at the last day; a label
// too close to it makes way.
const xAxisLabels = computed(() => {
  const pts = points.value
  const step = Math.max(1, Math.ceil(pts.length / 6))
  const labels = pts.filter((_, i) => i % step === 0)
  const last = pts.at(-1)
  if (last && labels.at(-1) !== last) {
    if (last.x - labels.at(-1).x < 10) labels.pop()
    labels.push(last)
  }
  return labels.map((p) => ({ x: p.x, label: p.date }))
})

function formatValue(v) {
  const sign = v < 0 ? '-' : ''
  const n = Math.abs(v)
  if (n >= 1000000) return sign + (n / 1000000).toFixed(1) + 'M'
  if (n >= 1000) return sign + (n / 1000).toFixed(1) + 'k'
  return String(v)
}

function labelShift(x) {
  if (x > 80) return 'translateX(-100%)'
  if (x < 20) return 'translateX(0)'
  return 'translateX(-50%)'
}
</script>

<template>
  <div class="w-full h-full flex flex-col">
    <div class="flex-1 relative min-h-0">
      <svg
        class="w-full h-full overflow-hidden"
        viewBox="0 0 100 100"
        preserveAspectRatio="none"
        data-test="chart"
        @mousemove="handleMouseMove"
        @mouseleave="hoveredPoint = null"
      >
        <line
          v-for="y in [25, 50, 75]"
          :key="y"
          x1="0"
          :y1="y"
          x2="100"
          :y2="y"
          stroke="currentColor"
          class="text-gray-200 dark:text-zinc-800"
          stroke-width="1"
          vector-effect="non-scaling-stroke"
          stroke-dasharray="2,3"
        />
        <line
          v-if="hoveredPoint"
          :x1="hoveredPoint.x"
          y1="15"
          :x2="hoveredPoint.x"
          y2="85"
          stroke="currentColor"
          class="text-gray-300 dark:text-zinc-700"
          stroke-width="1"
          vector-effect="non-scaling-stroke"
          stroke-dasharray="2,3"
        />
        <path
          v-if="linePath"
          :d="linePath"
          fill="none"
          :stroke="color"
          stroke-width="1.5"
          vector-effect="non-scaling-stroke"
          stroke-linecap="round"
          stroke-linejoin="round"
          data-test="chart-line"
        />
        <circle v-if="hoveredPoint" :cx="hoveredPoint.x" :cy="hoveredPoint.y" r="1.5" :fill="color" />
      </svg>

      <!-- Y-axis labels -->
      <div
        v-if="points.length"
        class="absolute pointer-events-none flex flex-col justify-between"
        style="top: 15%; bottom: 15%; left: 0; right: 0"
      >
        <span class="text-[9px] font-black text-gray-400 dark:text-zinc-500 tabular-nums leading-none" data-test="chart-max">{{
          formatValue(maxValue)
        }}</span>
        <span class="text-[9px] font-black text-gray-400 dark:text-zinc-500 tabular-nums leading-none" data-test="chart-min">{{
          formatValue(minValue)
        }}</span>
      </div>

      <div
        v-if="hoveredPoint"
        class="absolute z-20 bg-gray-900 dark:bg-zinc-900 text-white dark:text-zinc-100 border border-gray-700 dark:border-zinc-700 px-2.5 py-1 text-[10px] font-black uppercase tracking-widest pointer-events-none rounded whitespace-nowrap"
        :style="{
          left: `${hoveredPoint.x}%`,
          top: `${hoveredPoint.y}%`,
          transform: `${labelShift(hoveredPoint.x)} translateY(${hoveredPoint.y < 20 ? '10%' : '-110%'})`,
        }"
        data-test="chart-tooltip"
      >
        {{ hoveredPoint.date }}: {{ hoveredPoint.count }}
      </div>

      <div v-if="points.length === 0" class="absolute inset-0 flex items-center justify-center">
        <span class="text-[10px] font-black text-gray-300 dark:text-zinc-500 uppercase tracking-widest italic">No data points</span>
      </div>
    </div>

    <div v-if="xAxisLabels.length" class="relative h-5 mt-0.5 shrink-0">
      <span
        v-for="lbl in xAxisLabels"
        :key="lbl.x"
        class="absolute bottom-0 text-[9px] font-black text-gray-400 dark:text-zinc-500 tabular-nums leading-none whitespace-nowrap"
        :style="{ left: `${lbl.x}%`, transform: labelShift(lbl.x) }"
        data-test="chart-x-label"
        >{{ lbl.label }}</span
      >
    </div>
  </div>
</template>
