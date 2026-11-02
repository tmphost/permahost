<!--
  Copyright 2026 Contextual, Inc. https://permahost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
// The admin user list, newest first, searchable by email or name.
import { onMounted, ref } from 'vue'
import { listAdminUsers } from '../api'
import { HISTORY_DAYS } from '../utils/credits'
import { formatBytes } from '../utils/files'

const emit = defineEmits(['open', 'error'])

const INPUT =
  'w-full rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-3 py-2 text-sm text-gray-900 dark:text-zinc-100 placeholder:text-gray-400 dark:placeholder:text-zinc-500 focus:border-gray-400 dark:focus:border-zinc-500 focus:outline-none'
const SECONDARY =
  'rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-3 py-2 text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100 hover:border-gray-300 dark:hover:border-zinc-700 disabled:opacity-50 transition-colors'
// Columns: user, balance, used, folders; storage, domains and the joined
// date from md up.
const ROW =
  'grid grid-cols-[minmax(0,1fr)_4.5rem_4.5rem_3.5rem] md:grid-cols-[minmax(0,1fr)_5.5rem_5.5rem_4.5rem_5.5rem_4.5rem_7rem] items-center gap-3 px-4'

const query = ref('')
// The query the shown list answers.
const searched = ref('')
const users = ref([])
const next = ref('')
const loading = ref(true)
const loadingMore = ref(false)

async function search() {
  loading.value = true
  searched.value = query.value.trim()
  try {
    const page = await listAdminUsers({ q: searched.value })
    users.value = page.users
    next.value = page.next
  } catch (err) {
    emit('error', err)
  } finally {
    loading.value = false
  }
}

async function loadMore() {
  loadingMore.value = true
  try {
    const page = await listAdminUsers({ q: searched.value, before: next.value })
    users.value.push(...page.users)
    next.value = page.next
  } catch (err) {
    emit('error', err)
  } finally {
    loadingMore.value = false
  }
}

function clear() {
  query.value = ''
  search()
}

function formatDate(iso) {
  return new Date(iso).toLocaleDateString(undefined, { dateStyle: 'medium' })
}

onMounted(search)
</script>

<template>
  <div class="flex flex-col gap-4" data-test="admin-users">
    <form class="flex gap-2" role="search" data-test="admin-search" @submit.prevent="search">
      <label for="admin-user-query" class="sr-only">Search users</label>
      <input id="admin-user-query" v-model="query" type="search" placeholder="Email or name" :class="INPUT" data-test="admin-search-input" />
      <button type="submit" :class="SECONDARY" :disabled="loading" data-test="admin-search-submit">Search</button>
      <button v-if="searched" type="button" :class="SECONDARY" data-test="admin-search-clear" @click="clear">Clear</button>
    </form>

    <section class="bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl overflow-hidden">
      <div
        :class="[ROW, 'py-2.5 border-b border-gray-100 dark:border-zinc-800 text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500']"
      >
        <span>User</span>
        <span class="text-right">Balance</span>
        <span class="text-right">Used {{ HISTORY_DAYS }}d</span>
        <span class="text-right">Folders</span>
        <span class="hidden md:block text-right">Storage</span>
        <span class="hidden md:block text-right">Domains</span>
        <span class="hidden md:block text-right">Joined</span>
      </div>
      <p v-if="loading" class="px-4 py-6 text-[10px] font-black text-gray-300 dark:text-zinc-500 animate-pulse" data-test="admin-users-loading">
        Loading...
      </p>
      <p v-else-if="users.length === 0" class="px-4 py-6 text-xs text-gray-400 dark:text-zinc-500" data-test="admin-users-empty">
        {{ searched ? `No user matches “${searched}”.` : 'No users yet.' }}
      </p>
      <ul v-else class="divide-y divide-gray-100 dark:divide-zinc-800">
        <li v-for="u in users" :key="u.id">
          <button
            type="button"
            :class="[ROW, 'w-full py-2.5 text-left text-xs hover:bg-zinc-50 dark:hover:bg-zinc-800/50 transition-colors']"
            :data-test="`admin-user-${u.id}`"
            @click="emit('open', u.id)"
          >
            <span class="min-w-0">
              <span class="block truncate font-semibold text-gray-900 dark:text-zinc-100">{{ u.name || u.email }}</span>
              <span class="block truncate text-[10px] text-gray-400 dark:text-zinc-500">
                <template v-if="u.name">{{ u.email }} · </template>{{ u.creditsLimit.toLocaleString() }} / month
              </span>
            </span>
            <span class="text-right">
              <span
                :class="['block tabular-nums font-bold', u.paused ? 'text-rose-600 dark:text-rose-400' : 'text-gray-900 dark:text-zinc-100']"
                :data-test="`admin-user-balance-${u.id}`"
                >{{ u.credits.toLocaleString() }}</span
              >
              <span v-if="u.paused" class="block text-[10px] font-bold text-rose-600 dark:text-rose-400" :data-test="`admin-user-paused-${u.id}`"
                >Paused</span
              >
            </span>
            <span class="text-right tabular-nums text-gray-500 dark:text-zinc-400">{{ u.used.toLocaleString() }}</span>
            <span class="text-right tabular-nums text-gray-500 dark:text-zinc-400">{{ u.usage.folders }}</span>
            <span class="hidden md:block text-right tabular-nums text-gray-500 dark:text-zinc-400">{{ formatBytes(u.usage.storageBytes) }}</span>
            <span class="hidden md:block text-right tabular-nums text-gray-500 dark:text-zinc-400">{{ u.usage.domains }}</span>
            <span class="hidden md:block text-right text-gray-500 dark:text-zinc-400">{{ formatDate(u.createdAt) }}</span>
          </button>
        </li>
      </ul>
      <div v-if="next && !loading" class="px-4 py-3 border-t border-gray-100 dark:border-zinc-800">
        <button type="button" :class="SECONDARY" :disabled="loadingMore" data-test="admin-users-more" @click="loadMore">Load more</button>
      </div>
    </section>
  </div>
</template>
