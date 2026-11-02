<!--
  Copyright 2026 Contextual, Inc. https://permahost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
import { computed, onMounted, ref } from 'vue'
import DeleteModal from './DeleteModal.vue'
import { createDomain, deleteDomain, listDomains, verifyDomain } from '../api'
import { useClipboard } from '../composables/useClipboard'
import { useToasts } from '../composables/useToasts'

const props = defineProps({
  folder: { type: Object, required: true },
  // The pricing from the limits; null while they load.
  pricing: { type: Object, default: null },
})
// changed fires when credits were spent or a monthly charge went away.
const emit = defineEmits(['changed'])

const BUTTON =
  'rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-2.5 py-1.5 text-[10px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100 hover:border-gray-300 dark:hover:border-zinc-600 disabled:opacity-50 transition-colors'
const ICON_BUTTON =
  'inline-flex items-center justify-center size-6 shrink-0 rounded-lg text-gray-400 dark:text-zinc-500 hover:text-gray-900 dark:hover:text-zinc-100 hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors'

const { notifyError, notifySuccess } = useToasts()
const { copy } = useClipboard()

const loading = ref(true)
const domains = ref([])
const hostname = ref('')
const adding = ref(false)
// The ID of the domain being verified.
const verifying = ref(null)
const pendingDelete = ref(null)
const deleting = ref(false)

const priceHint = computed(() =>
  props.pricing
    ? `${props.pricing.domainSetupCredits} credits to set up and ${props.pricing.domainCredits} a month, charged once the CNAME is verified.`
    : '',
)

async function load() {
  try {
    domains.value = await listDomains(props.folder.id)
  } catch (err) {
    notifyError(err.message)
  } finally {
    loading.value = false
  }
}

function replace(domain) {
  domains.value = domains.value.map((d) => (d.id === domain.id ? domain : d))
}

async function add() {
  const name = hostname.value.trim()
  if (!name) return
  adding.value = true
  try {
    domains.value = [...domains.value, await createDomain(props.folder.id, name)]
    hostname.value = ''
    notifySuccess(`Added ${name}. Point its CNAME at the target, then verify.`)
  } catch (err) {
    notifyError(err.message)
  } finally {
    adding.value = false
  }
}

async function verify(domain) {
  verifying.value = domain.id
  try {
    replace(await verifyDomain(props.folder.id, domain.id))
    notifySuccess(`${domain.hostname} is active. Its certificate is issued within a minute.`)
    emit('changed')
  } catch (err) {
    notifyError(err.message)
    // The check is recorded on the domain, with what went wrong.
    await load()
  } finally {
    verifying.value = null
  }
}

async function confirmDelete() {
  const domain = pendingDelete.value
  deleting.value = true
  try {
    await deleteDomain(props.folder.id, domain.id)
    domains.value = domains.value.filter((d) => d.id !== domain.id)
    notifySuccess('Domain removed')
    emit('changed')
  } catch (err) {
    notifyError(err.message)
  } finally {
    deleting.value = false
    pendingDelete.value = null
  }
}

onMounted(load)
</script>

<template>
  <div class="border-t border-gray-100 dark:border-zinc-800 px-3 py-2" data-test="folder-domains">
    <p class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Custom domains</p>
    <p v-if="loading" class="py-1 text-xs text-gray-400 dark:text-zinc-500">Loading domains…</p>
    <ul v-else-if="domains.length" class="mt-1 divide-y divide-gray-50 dark:divide-zinc-800">
      <li v-for="domain in domains" :key="domain.id" class="py-1.5" :data-test="`domain-${domain.hostname}`">
        <div class="flex items-center gap-2">
          <a
            v-if="domain.status === 'active'"
            :href="`https://${domain.hostname}/`"
            target="_blank"
            rel="noopener"
            class="grow min-w-0 truncate font-mono text-xs text-gray-900 dark:text-zinc-100 hover:underline"
            data-test="domain-link"
          >{{ domain.hostname }}</a>
          <span v-else class="grow min-w-0 truncate font-mono text-xs text-gray-900 dark:text-zinc-100">{{ domain.hostname }}</span>
          <span
            :class="[
              'shrink-0 rounded px-1.5 py-0.5 text-[9px] font-black uppercase tracking-widest',
              domain.status === 'active'
                ? 'bg-emerald-50 dark:bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
                : 'bg-amber-50 dark:bg-amber-500/10 text-amber-600 dark:text-amber-400',
            ]"
            data-test="domain-status"
          >
            {{ domain.status === 'active' ? 'Active' : 'Pending' }}
          </span>
          <button
            type="button"
            :class="[ICON_BUTTON, 'hover:text-rose-600 dark:hover:text-rose-400 hover:bg-rose-50 dark:hover:bg-rose-500/10']"
            :title="`Remove ${domain.hostname}`"
            data-test="domain-delete"
            @click="pendingDelete = domain"
          >
            <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M6 18 18 6M6 6l12 12" />
            </svg>
          </button>
        </div>
        <div v-if="domain.status !== 'active'" class="mt-1 space-y-1">
          <div class="flex items-center gap-1 text-[11px] text-gray-500 dark:text-zinc-400">
            <span class="shrink-0">CNAME →</span>
            <span class="grow min-w-0 truncate font-mono text-gray-900 dark:text-zinc-100" data-test="domain-target">{{ domain.target }}</span>
            <button type="button" :class="ICON_BUTTON" title="Copy CNAME target" data-test="domain-copy" @click="copy(domain.target, 'CNAME target copied')">
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  d="M8 8V5a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1h-3M5 8h10a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V9a1 1 0 0 1 1-1z"
                />
              </svg>
            </button>
          </div>
          <p v-if="domain.lastError" class="text-[11px] text-rose-600 dark:text-rose-400" data-test="domain-error">{{ domain.lastError }}</p>
          <button
            type="button"
            :disabled="verifying === domain.id"
            :class="BUTTON"
            data-test="domain-verify"
            @click="verify(domain)"
          >
            {{ verifying === domain.id ? 'Checking…' : 'Verify now' }}
          </button>
        </div>
      </li>
    </ul>

    <form class="mt-2 flex items-center gap-2" data-test="domain-form" @submit.prevent="add">
      <input
        v-model="hostname"
        type="text"
        placeholder="www.example.com"
        aria-label="Domain name"
        autocomplete="off"
        spellcheck="false"
        class="grow min-w-0 rounded-md border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-2 py-1 font-mono text-xs text-gray-900 dark:text-zinc-100 placeholder:text-gray-400 dark:placeholder:text-zinc-500 focus:border-gray-400 dark:focus:border-zinc-500 focus:outline-none"
        data-test="domain-input"
      />
      <button type="submit" :disabled="adding || !hostname.trim()" :class="BUTTON" data-test="domain-add">Add</button>
    </form>
    <p class="mt-1 text-[11px] text-gray-400 dark:text-zinc-500" data-test="domain-pricing">
      Domains are checked every few minutes. {{ priceHint }}
    </p>

    <DeleteModal
      :open="Boolean(pendingDelete)"
      title="Remove domain?"
      confirm-label="Remove"
      :message="pendingDelete ? `${pendingDelete.hostname} stops serving this folder, and its monthly charge stops.` : ''"
      :busy="deleting"
      @confirm="confirmDelete"
      @cancel="pendingDelete = null"
    />
  </div>
</template>
