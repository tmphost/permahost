<!--
  Copyright 2026 Contextual, Inc. https://permahost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
// One user as root sees them: credits, usage, charts, a form to grant or
// deduct credits with a note, and the credit history.
import { computed, onMounted, ref } from 'vue'
import CreditHistory from './CreditHistory.vue'
import StatCharts from './StatCharts.vue'
import StatTile from './StatTile.vue'
import { adjustUserCredits, fetchAdminUser, listAdminUserCreditActivities } from '../api'
import { useCreditHistory } from '../composables/useCreditHistory'
import { useToasts } from '../composables/useToasts'
import { CHART_COLORS, HISTORY_DAYS, dailyCreditSeries, renewsIn } from '../utils/credits'
import { formatBytes } from '../utils/files'

const props = defineProps({
  // The user's base62 ID.
  id: { type: String, required: true },
})
const emit = defineEmits(['back', 'error'])

// MAX_NOTE mirrors the server's admin.MaxNoteLength.
const MAX_NOTE = 500

const INPUT =
  'w-full rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-3 py-2 text-sm text-gray-900 dark:text-zinc-100 placeholder:text-gray-400 dark:placeholder:text-zinc-500 focus:border-gray-400 dark:focus:border-zinc-500 focus:outline-none'
const LABEL = 'block text-[11px] font-bold text-gray-500 dark:text-zinc-400 mb-1'
const SECONDARY =
  'rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-3 py-2 text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100 hover:border-gray-300 dark:hover:border-zinc-700 transition-colors'
const PRIMARY =
  'rounded-lg bg-black dark:bg-white px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-white dark:text-zinc-900 hover:bg-gray-800 dark:hover:bg-zinc-200 disabled:opacity-50 transition-colors'

const MODES = [
  { id: 'grant', label: 'Grant' },
  { id: 'deduct', label: 'Deduct' },
]

const { notifySuccess } = useToasts()

const user = ref(null)
const missing = ref(false)
const failed = ref(false)

const mode = ref('grant')
const amount = ref(null)
const note = ref('')
const saving = ref(false)

const { activities, next, loading, loadingOlder, loadRange, loadOlder } = useCreditHistory(
  (before) => listAdminUserCreditActivities(props.id, before),
  (err) => emit('error', err),
)

const series = computed(() => dailyCreditSeries(activities.value))
const charts = computed(() => [
  { id: 'used', title: 'Credits used', hint: 'Per day', color: CHART_COLORS.used, data: series.value.used },
  { id: 'balance', title: 'Balance', hint: 'End of day', color: CHART_COLORS.balance, data: series.value.balance },
])

const validAmount = computed(() => Number.isInteger(amount.value) && amount.value > 0)
// A deduction may not take the balance below zero.
const maxDeduction = computed(() => Math.max(0, user.value.credits))
const overdrawn = computed(() => mode.value === 'deduct' && validAmount.value && amount.value > maxDeduction.value)
const balanceAfter = computed(() => user.value.credits + (mode.value === 'grant' ? amount.value : -amount.value))
const canSave = computed(() => validAmount.value && !overdrawn.value && note.value.trim() !== '' && !saving.value)

function formatDate(iso) {
  return new Date(iso).toLocaleDateString(undefined, { dateStyle: 'medium' })
}

async function save() {
  saving.value = true
  const signed = mode.value === 'grant' ? amount.value : -amount.value
  try {
    const rs = await adjustUserCredits(props.id, signed, note.value.trim())
    user.value = rs.user
    activities.value.unshift(rs.activity)
    notifySuccess(
      `${mode.value === 'grant' ? 'Granted' : 'Deducted'} ${amount.value.toLocaleString()} credits. Balance: ${rs.activity.current.toLocaleString()}.`,
    )
    amount.value = null
    note.value = ''
  } catch (err) {
    emit('error', err)
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  try {
    user.value = await fetchAdminUser(props.id)
  } catch (err) {
    if (err.status === 404) {
      missing.value = true
      return
    }
    failed.value = true
    emit('error', err)
    return
  }
  await loadRange()
})
</script>

<template>
  <div class="flex flex-col gap-4" data-test="admin-user">
    <div>
      <button type="button" :class="SECONDARY" data-test="admin-user-back" @click="emit('back')">← All users</button>
    </div>

    <p v-if="missing" class="rounded-xl border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-4 py-6 text-xs text-gray-500 dark:text-zinc-400" data-test="admin-user-missing">
      No user has the ID <span class="font-mono">{{ id }}</span>.
    </p>

    <p v-else-if="failed" class="rounded-xl border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-4 py-6 text-xs text-gray-500 dark:text-zinc-400" data-test="admin-user-failed">
      This user could not be loaded.
    </p>

    <template v-else-if="user">
      <header class="flex flex-wrap items-center gap-3 rounded-xl border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-4">
        <img v-if="user.picture" :src="user.picture" alt="" class="w-10 h-10 rounded-full shrink-0" referrerpolicy="no-referrer" />
        <div class="grow min-w-0">
          <h2 class="text-sm font-bold text-gray-900 dark:text-zinc-100 truncate" data-test="admin-user-name">{{ user.name || user.email }}</h2>
          <p class="text-xs text-gray-500 dark:text-zinc-400 truncate">{{ user.email }}</p>
          <p class="text-[10px] text-gray-400 dark:text-zinc-500">
            <span class="font-mono" data-test="admin-user-id">{{ user.id }}</span>
            <template v-if="user.provider"> · {{ user.provider }}</template> · joined {{ formatDate(user.createdAt) }}
          </p>
        </div>
      </header>

      <p
        v-if="user.paused"
        class="rounded-lg border border-rose-100 dark:border-rose-500/20 bg-rose-50 dark:bg-rose-500/10 px-3 py-2 text-xs font-medium text-rose-600 dark:text-rose-400"
        data-test="admin-user-paused"
      >
        Sites paused: the balance is below zero. Grant at least {{ (-user.credits).toLocaleString() }} credits to resume them now.
      </p>

      <div class="grid grid-cols-2 md:grid-cols-4 xl:grid-cols-7 gap-4">
        <StatTile label="Balance" :value="user.credits.toLocaleString()" :color="CHART_COLORS.balance" test="admin-user-balance" />
        <StatTile :label="`Used, ${HISTORY_DAYS} days`" :value="user.used.toLocaleString()" :color="CHART_COLORS.used" test="admin-user-used" />
        <StatTile label="Grant / month" :value="user.creditsLimit.toLocaleString()" test="admin-user-monthly">
          <span class="text-[10px] text-gray-400 dark:text-zinc-500" data-test="admin-user-renewal">{{ renewsIn(user.creditsRenewAt) }}</span>
        </StatTile>
        <StatTile label="Cost / month" :value="user.usage.monthlyCredits.toLocaleString()" test="admin-user-cost" />
        <StatTile label="Folders" :value="user.usage.folders" test="admin-user-folders" />
        <StatTile label="Storage" :value="formatBytes(user.usage.storageBytes)" test="admin-user-storage" />
        <StatTile class="col-span-2 md:col-span-1" label="Domains" :value="user.usage.domains" test="admin-user-domains" />
      </div>

      <StatCharts :charts="charts" :loading="loading" />

      <form
        class="rounded-xl border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-5"
        data-test="admin-adjust"
        @submit.prevent="save"
      >
        <h2 class="text-sm font-bold text-gray-900 dark:text-zinc-100">Adjust credits</h2>
        <p class="text-xs text-gray-500 dark:text-zinc-400 mb-4">
          Changes the balance hosting is charged from, until the next renewal resets it to the monthly grant minus the monthly
          cost. A balance at zero or more resumes paused sites at once.
        </p>

        <div class="space-y-4">
          <div class="inline-flex rounded-lg border border-gray-200 dark:border-zinc-800 p-0.5" role="radiogroup" aria-label="Grant or deduct">
            <button
              v-for="m in MODES"
              :key="m.id"
              type="button"
              role="radio"
              :aria-checked="mode === m.id"
              :class="[
                'px-3 py-1.5 rounded-md text-xs transition-colors',
                mode === m.id
                  ? 'bg-gray-200/70 dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 font-semibold'
                  : 'text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-100',
              ]"
              :data-test="`admin-mode-${m.id}`"
              @click="mode = m.id"
            >
              {{ m.label }}
            </button>
          </div>

          <div class="grid grid-cols-1 md:grid-cols-[12rem_minmax(0,1fr)] gap-3">
            <div>
              <label for="admin-adjust-amount" :class="LABEL">Credits</label>
              <input
                id="admin-adjust-amount"
                v-model.number="amount"
                type="number"
                min="1"
                step="1"
                required
                placeholder="500"
                :class="INPUT"
                data-test="admin-adjust-amount"
              />
              <p
                class="mt-1 text-[11px]"
                :class="overdrawn ? 'text-rose-600 dark:text-rose-400' : 'text-gray-400 dark:text-zinc-500'"
                data-test="admin-adjust-preview"
              >
                <template v-if="overdrawn">At most {{ maxDeduction.toLocaleString() }} can be deducted.</template>
                <template v-else-if="validAmount">Balance after: {{ balanceAfter.toLocaleString() }}</template>
                <template v-else>A whole number above zero.</template>
              </p>
            </div>
            <div>
              <label for="admin-adjust-note" :class="LABEL">Note (required)</label>
              <input
                id="admin-adjust-note"
                v-model="note"
                required
                :maxlength="MAX_NOTE"
                placeholder="Why, e.g. refund for the outage on 1 Nov"
                :class="INPUT"
                data-test="admin-adjust-note"
              />
              <p class="mt-1 text-[11px] text-gray-400 dark:text-zinc-500">Kept in the history below.</p>
            </div>
          </div>

          <button type="submit" :disabled="!canSave" :class="PRIMARY" data-test="admin-adjust-save">
            {{ saving ? 'Saving...' : mode === 'grant' ? 'Grant credits' : 'Deduct credits' }}
          </button>
        </div>
      </form>

      <CreditHistory :activities="activities" :next="next" :loading="loading" :loading-older="loadingOlder" @older="loadOlder" />
    </template>

    <p v-else class="px-1 py-6 text-[10px] font-black text-gray-300 dark:text-zinc-500 animate-pulse" data-test="admin-user-loading">Loading...</p>
  </div>
</template>
