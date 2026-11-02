<!--
  Copyright 2026 Contextual, Inc. https://permahost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
// The admin stats over every account: totals and the last 30 days.
import { computed, onMounted, ref } from 'vue'
import StatCharts from './StatCharts.vue'
import StatTile from './StatTile.vue'
import { fetchAdminStats } from '../api'
import { CHART_COLORS, HISTORY_DAYS } from '../utils/credits'

const emit = defineEmits(['error'])

const stats = ref(null)
const loading = ref(true)

const charts = computed(() => {
  const days = stats.value?.days ?? []
  return [
    { id: 'used', title: 'Credits used', hint: 'Per day', color: CHART_COLORS.used, data: days.map((d) => ({ date: d.date, count: d.used })) },
    { id: 'signups', title: 'Sign-ups', hint: 'Per day', color: CHART_COLORS.balance, data: days.map((d) => ({ date: d.date, count: d.signups })) },
  ]
})

function count(n) {
  return (n ?? 0).toLocaleString()
}

onMounted(async () => {
  try {
    stats.value = await fetchAdminStats()
  } catch (err) {
    emit('error', err)
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div class="flex flex-col gap-4" data-test="admin-overview">
    <div class="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-4">
      <StatTile label="Users" :value="count(stats?.users)" test="admin-stat-users" />
      <StatTile label="Credits held" :value="count(stats?.credits)" test="admin-stat-credits" />
      <StatTile :label="`Used, ${HISTORY_DAYS}d`" :value="count(stats?.used)" :color="CHART_COLORS.used" test="admin-stat-used" />
      <StatTile :label="`Sign-ups, ${HISTORY_DAYS}d`" :value="count(stats?.signups)" :color="CHART_COLORS.balance" test="admin-stat-signups" />
      <StatTile label="Folders" :value="count(stats?.folders)" test="admin-stat-folders" />
      <StatTile label="Paused accounts" :value="count(stats?.paused)" test="admin-stat-paused" />
    </div>
    <StatCharts :charts="charts" :loading="loading" test-prefix="admin" />
  </div>
</template>
