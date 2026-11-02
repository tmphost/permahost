<!--
  Copyright 2026 Contextual, Inc. https://permahost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
// One user's credits as root sees them: balance and usage, a form to grant
// or deduct credits with a note, and the history of those adjustments.
import { computed, onMounted, ref } from 'vue'
import { adjustUserCredits, listUserCreditAdjustments } from '../api'
import { useToasts } from '../composables/useToasts'
import { renewsIn } from '../utils/credits'
import { formatBytes } from '../utils/files'

const props = defineProps({
  // A user from the setup user list.
  user: { type: Object, required: true },
})
// updated: the user after an adjustment. expired: the root session ended.
const emit = defineEmits(['back', 'updated', 'expired'])

const MAX_NOTE = 500
const INPUT =
  'w-full rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-3 py-2 text-sm text-gray-900 dark:text-zinc-100 placeholder:text-gray-400 dark:placeholder:text-zinc-500 focus:border-gray-400 dark:focus:border-zinc-500 focus:outline-none'
const LABEL = 'block text-[11px] font-bold text-gray-500 dark:text-zinc-400 mb-1'
const PRIMARY =
  'rounded-lg bg-black dark:bg-white px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-white dark:text-zinc-900 hover:bg-gray-800 dark:hover:bg-zinc-200 disabled:opacity-50 transition-colors'
const SECONDARY =
  'rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-3 py-2 text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100 hover:border-gray-300 dark:hover:border-zinc-600 disabled:opacity-50 transition-colors'

const { notifyError, notifySuccess } = useToasts()

// 'grant' | 'deduct'
const mode = ref('grant')
const amount = ref(null)
const note = ref('')
const saving = ref(false)

const adjustments = ref([])
const next = ref('')
const loading = ref(true)
const loadingOlder = ref(false)

const validAmount = computed(() => Number.isInteger(amount.value) && amount.value > 0)
// A deduction may not take the balance below zero.
const maxDeduction = computed(() => Math.max(0, props.user.credits))
const overdrawn = computed(() => mode.value === 'deduct' && validAmount.value && amount.value > maxDeduction.value)
const balanceAfter = computed(() => props.user.credits + (mode.value === 'grant' ? amount.value : -amount.value))
const canSave = computed(() => validAmount.value && !overdrawn.value && note.value.trim() !== '' && !saving.value)

const stats = computed(() => [
  { key: 'grant', label: 'Monthly grant', value: props.user.creditsLimit.toLocaleString(), hint: renewsIn(props.user.creditsRenewAt) },
  { key: 'cost', label: 'Monthly cost', value: props.user.usage.monthlyCredits.toLocaleString() },
  { key: 'folders', label: 'Folders', value: props.user.usage.folders },
  { key: 'storage', label: 'Storage', value: formatBytes(props.user.usage.storageBytes) },
  { key: 'domains', label: 'Domains', value: props.user.usage.domains },
])

function fail(err) {
  if (err.status === 401) emit('expired')
  else notifyError(err.message)
}

async function load(before = '') {
  try {
    const page = await listUserCreditAdjustments(props.user.id, before)
    adjustments.value = before ? [...adjustments.value, ...page.adjustments] : page.adjustments
    next.value = page.next
  } catch (err) {
    fail(err)
  }
}

async function loadOlder() {
  loadingOlder.value = true
  await load(next.value)
  loadingOlder.value = false
}

async function save() {
  saving.value = true
  const signed = mode.value === 'grant' ? amount.value : -amount.value
  try {
    const rs = await adjustUserCredits(props.user.id, signed, note.value.trim())
    adjustments.value = [rs.adjustment, ...adjustments.value]
    emit('updated', rs.user)
    notifySuccess(
      `${mode.value === 'grant' ? 'Granted' : 'Deducted'} ${amount.value.toLocaleString()} credits. Balance: ${rs.adjustment.balanceAfter.toLocaleString()}.`,
    )
    amount.value = null
    note.value = ''
  } catch (err) {
    fail(err)
  } finally {
    saving.value = false
  }
}

function formatAmount(n) {
  return `${n > 0 ? '+' : '−'}${Math.abs(n).toLocaleString()}`
}

function formatDate(iso) {
  return new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
}

onMounted(async () => {
  await load()
  loading.value = false
})
</script>

<template>
  <div class="flex flex-col gap-4" data-test="user-credits">
    <div>
      <button type="button" :class="SECONDARY" data-test="user-credits-back" @click="emit('back')">← All users</button>
    </div>

    <header class="flex flex-wrap items-center gap-3 rounded-xl border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-4">
      <img v-if="user.picture" :src="user.picture" alt="" class="w-10 h-10 rounded-full shrink-0" referrerpolicy="no-referrer" />
      <div class="grow min-w-0">
        <h2 class="text-sm font-bold text-gray-900 dark:text-zinc-100 truncate" data-test="user-credits-name">{{ user.name || user.email }}</h2>
        <p class="text-xs text-gray-500 dark:text-zinc-400 truncate">{{ user.email }}</p>
        <p class="text-[10px] text-gray-400 dark:text-zinc-500">
          <span class="font-mono">{{ user.id }}</span><template v-if="user.provider"> · {{ user.provider }}</template>
        </p>
      </div>
      <div class="text-right">
        <p class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Balance</p>
        <p
          class="text-xl font-black tabular-nums"
          :class="user.paused ? 'text-rose-600 dark:text-rose-400' : 'text-gray-900 dark:text-zinc-100'"
          data-test="user-credits-balance"
        >
          {{ user.credits.toLocaleString() }}
        </p>
      </div>
    </header>

    <p
      v-if="user.paused"
      class="rounded-lg border border-rose-100 dark:border-rose-500/20 bg-rose-50 dark:bg-rose-500/10 px-3 py-2 text-xs font-medium text-rose-600 dark:text-rose-400"
      data-test="user-credits-paused"
    >
      Sites paused: the balance is below zero. Grant at least {{ (-user.credits).toLocaleString() }} credits to resume them now.
    </p>

    <div class="grid grid-cols-2 sm:grid-cols-5 gap-2">
      <div
        v-for="s in stats"
        :key="s.key"
        class="rounded-xl border border-gray-100 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-3 py-2.5"
        :data-test="`user-credits-${s.key}`"
      >
        <p class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">{{ s.label }}</p>
        <p class="text-sm font-bold tabular-nums text-gray-900 dark:text-zinc-100">{{ s.value }}</p>
        <p v-if="s.hint" class="text-[10px] text-gray-400 dark:text-zinc-500">{{ s.hint }}</p>
      </div>
    </div>

    <form
      class="rounded-xl border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-5"
      data-test="user-credits-form"
      @submit.prevent="save"
    >
      <h3 class="text-sm font-bold text-gray-900 dark:text-zinc-100">Adjust credits</h3>
      <p class="text-xs text-gray-500 dark:text-zinc-400 mb-4">
        Changes the balance hosting is charged from, until the next renewal resets it to the monthly grant minus the monthly cost.
      </p>
      <div class="inline-flex rounded-lg border border-gray-200 dark:border-zinc-800 p-0.5 mb-4" role="radiogroup" aria-label="Grant or deduct">
        <button
          v-for="option in [{ value: 'grant', label: 'Grant' }, { value: 'deduct', label: 'Deduct' }]"
          :key="option.value"
          type="button"
          role="radio"
          :aria-checked="mode === option.value"
          :class="[
            'rounded-md px-3 py-1.5 text-xs font-bold transition-colors',
            mode === option.value ? 'bg-black dark:bg-white text-white dark:text-zinc-900' : 'text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-100',
          ]"
          :data-test="`user-credits-mode-${option.value}`"
          @click="mode = option.value"
        >
          {{ option.label }}
        </button>
      </div>
      <div class="grid grid-cols-1 sm:grid-cols-[12rem_minmax(0,1fr)] gap-3">
        <div>
          <label for="adjust-amount" :class="LABEL">Credits</label>
          <input
            id="adjust-amount"
            v-model.number="amount"
            type="number"
            min="1"
            step="1"
            required
            placeholder="500"
            :class="INPUT"
            data-test="user-credits-amount"
          />
          <p
            class="mt-1 text-[11px]"
            :class="overdrawn ? 'text-rose-600 dark:text-rose-400' : 'text-gray-400 dark:text-zinc-500'"
            data-test="user-credits-preview"
          >
            <template v-if="overdrawn">At most {{ maxDeduction.toLocaleString() }} can be deducted.</template>
            <template v-else-if="validAmount">Balance after: {{ balanceAfter.toLocaleString() }}</template>
            <template v-else>A whole number above zero.</template>
          </p>
        </div>
        <div>
          <label for="adjust-note" :class="LABEL">Note (required)</label>
          <input
            id="adjust-note"
            v-model="note"
            required
            :maxlength="MAX_NOTE"
            placeholder="Why, e.g. refund for the outage on 1 Nov"
            :class="INPUT"
            data-test="user-credits-note"
          />
          <p class="mt-1 text-[11px] text-gray-400 dark:text-zinc-500">Kept in the history below.</p>
        </div>
      </div>
      <div class="mt-4 flex justify-end">
        <button type="submit" :disabled="!canSave" :class="PRIMARY" data-test="user-credits-save">
          {{ saving ? 'Saving...' : mode === 'grant' ? 'Grant credits' : 'Deduct credits' }}
        </button>
      </div>
    </form>

    <section class="rounded-xl border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 overflow-hidden" data-test="user-credits-history">
      <h3 class="px-4 py-3 border-b border-gray-100 dark:border-zinc-800 text-sm font-bold text-gray-900 dark:text-zinc-100">Adjustment history</h3>
      <p v-if="loading" class="px-4 py-6 text-[10px] font-black text-gray-300 dark:text-zinc-500 animate-pulse" data-test="user-credits-history-loading">
        Loading...
      </p>
      <p v-else-if="adjustments.length === 0" class="px-4 py-6 text-xs text-gray-400 dark:text-zinc-500" data-test="user-credits-history-empty">
        No adjustments yet.
      </p>
      <ul v-else class="divide-y divide-gray-100 dark:divide-zinc-800">
        <li v-for="a in adjustments" :key="a.id" class="flex items-start gap-3 px-4 py-2.5 text-xs" :data-test="`user-credits-entry-${a.id}`">
          <span
            class="w-16 shrink-0 tabular-nums font-bold"
            :class="a.amount > 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-rose-600 dark:text-rose-400'"
          >
            {{ formatAmount(a.amount) }}
          </span>
          <span class="grow min-w-0">
            <span class="block text-gray-900 dark:text-zinc-100 break-words">{{ a.note }}</span>
            <span class="block text-[10px] text-gray-400 dark:text-zinc-500">{{ a.actor }} · {{ formatDate(a.createdAt) }}</span>
          </span>
          <span class="shrink-0 text-right tabular-nums text-gray-500 dark:text-zinc-400">
            <span class="block text-[10px] text-gray-400 dark:text-zinc-500">Balance</span>
            {{ a.balanceAfter.toLocaleString() }}
          </span>
        </li>
      </ul>
      <div v-if="next && !loading" class="px-4 py-3 border-t border-gray-100 dark:border-zinc-800">
        <button type="button" :class="SECONDARY" :disabled="loadingOlder" data-test="user-credits-older" @click="loadOlder">Load older</button>
      </div>
    </section>
  </div>
</template>
