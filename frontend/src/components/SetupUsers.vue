<!--
  Copyright 2026 Contextual, Inc. https://permahost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
// The setup page's Users section: search users by email or name, then open
// one to grant or deduct credits.
import { onMounted, ref } from 'vue'
import SetupUserCredits from './SetupUserCredits.vue'
import { listSetupUsers } from '../api'
import { useToasts } from '../composables/useToasts'
import { formatBytes } from '../utils/files'

// expired: the root session ended; the page signs in again.
const emit = defineEmits(['expired'])

const INPUT =
  'w-full rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-3 py-2 text-sm text-gray-900 dark:text-zinc-100 placeholder:text-gray-400 dark:placeholder:text-zinc-500 focus:border-gray-400 dark:focus:border-zinc-500 focus:outline-none'
const SECONDARY =
  'rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-3 py-2 text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100 hover:border-gray-300 dark:hover:border-zinc-600 disabled:opacity-50 transition-colors'
// Columns: user, balance, folders, storage, domains; the last three from sm up.
const ROW =
  'grid grid-cols-[minmax(0,1fr)_5.5rem] sm:grid-cols-[minmax(0,1fr)_5.5rem_3.5rem_4.5rem_3.5rem] items-center gap-3 px-4'

const { notifyError } = useToasts()

const query = ref('')
// The query the shown list answers.
const searched = ref('')
const users = ref([])
const next = ref('')
const loading = ref(true)
const loadingMore = ref(false)
// The user whose credits panel is open.
const selected = ref(null)

function fail(err) {
  if (err.status === 401) emit('expired')
  else notifyError(err.message)
}

async function search() {
  loading.value = true
  searched.value = query.value.trim()
  try {
    const page = await listSetupUsers({ q: searched.value })
    users.value = page.users
    next.value = page.next
  } catch (err) {
    fail(err)
  } finally {
    loading.value = false
  }
}

async function loadMore() {
  loadingMore.value = true
  try {
    const page = await listSetupUsers({ q: searched.value, before: next.value })
    users.value.push(...page.users)
    next.value = page.next
  } catch (err) {
    fail(err)
  } finally {
    loadingMore.value = false
  }
}

function clear() {
  query.value = ''
  search()
}

// updated keeps the list in step with an adjustment made in the panel.
function updated(user) {
  users.value = users.value.map((u) => (u.id === user.id ? user : u))
  selected.value = user
}

onMounted(search)
</script>

<template>
  <SetupUserCredits v-if="selected" :user="selected" @back="selected = null" @updated="updated" @expired="emit('expired')" />

  <div v-else class="flex flex-col gap-4" data-test="setup-users">
    <form class="flex gap-2" role="search" data-test="users-search" @submit.prevent="search">
      <label for="users-query" class="sr-only">Search users</label>
      <input id="users-query" v-model="query" type="search" placeholder="Email or name" :class="INPUT" data-test="users-search-input" />
      <button type="submit" :class="SECONDARY" :disabled="loading" data-test="users-search-submit">Search</button>
      <button v-if="searched" type="button" :class="SECONDARY" data-test="users-search-clear" @click="clear">Clear</button>
    </form>

    <section class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl overflow-hidden">
      <div
        :class="[ROW, 'py-2.5 border-b border-gray-100 dark:border-zinc-800 text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500']"
      >
        <span>User</span>
        <span class="text-right">Balance</span>
        <span class="hidden sm:block text-right">Folders</span>
        <span class="hidden sm:block text-right">Storage</span>
        <span class="hidden sm:block text-right">Domains</span>
      </div>
      <p v-if="loading" class="px-4 py-6 text-[10px] font-black text-gray-300 dark:text-zinc-500 animate-pulse" data-test="users-loading">
        Loading...
      </p>
      <p v-else-if="users.length === 0" class="px-4 py-6 text-xs text-gray-400 dark:text-zinc-500" data-test="users-empty">
        {{ searched ? `No user matches “${searched}”.` : 'No users yet.' }}
      </p>
      <ul v-else class="divide-y divide-gray-100 dark:divide-zinc-800">
        <li v-for="u in users" :key="u.id">
          <button
            type="button"
            :class="[ROW, 'w-full py-2.5 text-left text-xs hover:bg-zinc-50 dark:hover:bg-zinc-800/50 transition-colors']"
            :data-test="`users-row-${u.id}`"
            @click="selected = u"
          >
            <span class="min-w-0">
              <span class="block truncate font-semibold text-gray-900 dark:text-zinc-100">{{ u.name || u.email }}</span>
              <span class="block truncate text-[10px] text-gray-400 dark:text-zinc-500">
                <template v-if="u.name">{{ u.email }} · </template>{{ u.creditsLimit.toLocaleString() }} / month
              </span>
            </span>
            <span class="text-right">
              <span
                class="block tabular-nums font-bold"
                :class="u.paused ? 'text-rose-600 dark:text-rose-400' : 'text-gray-900 dark:text-zinc-100'"
                :data-test="`users-balance-${u.id}`"
              >
                {{ u.credits.toLocaleString() }}
              </span>
              <span v-if="u.paused" class="block text-[10px] font-bold text-rose-600 dark:text-rose-400" :data-test="`users-paused-${u.id}`">Paused</span>
            </span>
            <span class="hidden sm:block text-right tabular-nums text-gray-500 dark:text-zinc-400">{{ u.usage.folders }}</span>
            <span class="hidden sm:block text-right tabular-nums text-gray-500 dark:text-zinc-400">{{ formatBytes(u.usage.storageBytes) }}</span>
            <span class="hidden sm:block text-right tabular-nums text-gray-500 dark:text-zinc-400">{{ u.usage.domains }}</span>
          </button>
        </li>
      </ul>
      <div v-if="next && !loading" class="px-4 py-3 border-t border-gray-100 dark:border-zinc-800">
        <button type="button" :class="SECONDARY" :disabled="loadingMore" data-test="users-more" @click="loadMore">Load more</button>
      </div>
    </section>
  </div>
</template>
