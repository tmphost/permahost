<!--
  Copyright 2026 Contextual, Inc. https://permahost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Commercial
-->

<script setup>
// Root's credit grant to one user, by email, or to every user at once. The
// credits expire 30 days later; renewals keep them until then.
import { computed, ref } from 'vue'
import { grantCredits } from '../api'
import { useToasts } from '../composables/useToasts'

const emit = defineEmits(['error'])

// MAX_GRANT mirrors the server's admin.MaxAdjustment, and MAX_NOTE its
// admin.MaxNoteLength.
const MAX_GRANT = 1000000
const MAX_NOTE = 500

const INPUT =
  'w-full rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-3 py-2 text-sm text-gray-900 dark:text-zinc-100 placeholder:text-gray-400 dark:placeholder:text-zinc-500 focus:border-gray-400 dark:focus:border-zinc-500 focus:outline-none'
const LABEL = 'block text-[11px] font-bold text-gray-500 dark:text-zinc-400 mb-1'
const PRIMARY =
  'rounded-lg bg-black dark:bg-white px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-white dark:text-zinc-900 hover:bg-gray-800 dark:hover:bg-zinc-200 disabled:opacity-50 transition-colors'

const TARGETS = [
  { id: 'all', label: 'All users' },
  { id: 'one', label: 'One user' },
]

const { notifySuccess } = useToasts()

const target = ref('all')
const email = ref('')
const credits = ref(100)
const note = ref('')
const busy = ref(false)
const grantError = ref('')
// The last grant: { credits, users, email, expiresAt }.
const result = ref(null)

const canGrant = computed(
  () =>
    Number.isInteger(credits.value) &&
    credits.value > 0 &&
    credits.value <= MAX_GRANT &&
    (target.value === 'all' || email.value.trim() !== '') &&
    note.value.trim() !== '' &&
    !busy.value,
)

function formatDate(iso) {
  return new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
}

function usersLabel(n) {
  return n === 1 ? '1 user' : `${n.toLocaleString()} users`
}

async function grant() {
  busy.value = true
  grantError.value = ''
  result.value = null
  const all = target.value === 'all'
  const to = all ? '' : email.value.trim()
  try {
    const rs = await grantCredits({ credits: credits.value, email: to, all, note: note.value.trim() })
    result.value = { credits: credits.value, users: rs.users, email: to, expiresAt: rs.expiresAt }
    notifySuccess(`Granted ${credits.value.toLocaleString()} credits to ${usersLabel(rs.users)}.`)
    note.value = ''
  } catch (err) {
    if (err.status === 401) {
      emit('error', err)
      return
    }
    grantError.value = err.message
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <form class="rounded-xl border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-5" data-test="admin-grant" @submit.prevent="grant">
    <h2 class="text-sm font-bold text-gray-900 dark:text-zinc-100">Grant credits</h2>
    <p class="text-xs text-gray-500 dark:text-zinc-400 mb-4">
      Granted credits expire 30 days later, at 00:00 UTC; whatever is left of them is taken back. Monthly renewals keep them and use them for
      any hosting cost the monthly grant can't cover. A balance at zero or more resumes paused sites at once.
    </p>

    <div class="space-y-4">
      <div>
        <span :class="LABEL">Grant to</span>
        <div class="inline-flex rounded-lg border border-gray-200 dark:border-zinc-800 p-0.5" role="radiogroup" aria-label="Grant to">
          <button
            v-for="t in TARGETS"
            :key="t.id"
            type="button"
            role="radio"
            :aria-checked="target === t.id"
            :class="[
              'px-3 py-1.5 rounded-md text-xs transition-colors',
              target === t.id
                ? 'bg-gray-200/70 dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 font-semibold'
                : 'text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-100',
            ]"
            :data-test="`admin-target-${t.id}`"
            @click="target = t.id"
          >
            {{ t.label }}
          </button>
        </div>
      </div>

      <div v-if="target === 'one'">
        <label for="admin-email" :class="LABEL">User email</label>
        <input id="admin-email" v-model="email" type="email" placeholder="user@example.com" :class="INPUT" data-test="admin-email" />
      </div>

      <div class="grid grid-cols-1 md:grid-cols-[12rem_minmax(0,1fr)] gap-3">
        <div>
          <label for="admin-credits" :class="LABEL">Credits</label>
          <input
            id="admin-credits"
            v-model.number="credits"
            type="number"
            min="1"
            :max="MAX_GRANT"
            step="1"
            :class="INPUT"
            data-test="admin-credits"
          />
          <p class="mt-1 text-[11px] text-gray-400 dark:text-zinc-500">Between 1 and {{ MAX_GRANT.toLocaleString() }} per user.</p>
        </div>
        <div>
          <label for="admin-grant-note" :class="LABEL">Note (required)</label>
          <input
            id="admin-grant-note"
            v-model="note"
            :maxlength="MAX_NOTE"
            placeholder="Why, e.g. launch bonus"
            :class="INPUT"
            data-test="admin-grant-note"
          />
          <p class="mt-1 text-[11px] text-gray-400 dark:text-zinc-500">Kept in each user's credit history.</p>
        </div>
      </div>
    </div>

    <p
      v-if="grantError"
      role="alert"
      class="mt-4 rounded-lg border border-rose-100 dark:border-rose-500/20 bg-rose-50 dark:bg-rose-500/10 px-3 py-2 text-xs font-medium text-rose-600 dark:text-rose-400"
      data-test="admin-grant-error"
    >
      {{ grantError }}
    </p>
    <p
      v-if="result"
      class="mt-4 rounded-lg border border-emerald-200 dark:border-emerald-500/30 bg-emerald-50 dark:bg-emerald-500/10 px-3 py-2 text-xs font-medium text-emerald-800 dark:text-emerald-300"
      data-test="admin-grant-result"
    >
      Granted {{ result.credits.toLocaleString() }} credits to {{ result.email || usersLabel(result.users) }}. They expire
      {{ formatDate(result.expiresAt) }}.
    </p>

    <div class="mt-5 flex justify-end">
      <button type="submit" :disabled="!canGrant" :class="PRIMARY" data-test="admin-grant-submit">
        {{ busy ? 'Granting...' : target === 'all' ? 'Grant to all users' : 'Grant' }}
      </button>
    </div>
  </form>
</template>
