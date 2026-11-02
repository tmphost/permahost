<!--
  Copyright 2026 Contextual, Inc. https://permahost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
import { computed, nextTick, ref, watch } from 'vue'
import { useClipboard } from '../composables/useClipboard'
import { recordName, registeredDomain } from '../utils/domains'

// DnsSetupModal walks through creating the CNAME record a custom domain
// needs. The parent verifies the domain and closes the modal by clearing
// domain.
const props = defineProps({
  // The domain to set up; null keeps the modal closed.
  domain: { type: Object, default: null },
  pricing: { type: Object, default: null },
  verifying: { type: Boolean, default: false },
})
const emit = defineEmits(['close', 'verify'])

const PRIMARY =
  'rounded-lg bg-black dark:bg-white px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-white dark:text-zinc-900 hover:bg-gray-800 dark:hover:bg-zinc-200 disabled:opacity-50 transition-colors'
const SECONDARY =
  'rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100 hover:border-gray-300 dark:hover:border-zinc-600 disabled:opacity-50 transition-colors'
const ICON_BUTTON =
  'inline-flex items-center justify-center size-6 shrink-0 rounded-lg text-gray-400 dark:text-zinc-500 hover:text-gray-900 dark:hover:text-zinc-100 hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors'
const COPY_ICON =
  'M8 8V5a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1h-3M5 8h10a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V9a1 1 0 0 1 1-1z'

const { copy } = useClipboard()
const dialog = ref(null)

// Read only while the modal is open, so domain is set.
const name = computed(() => recordName(props.domain.hostname))
const zone = computed(() => registeredDomain(props.domain.hostname))
// A registered domain itself (example.com) cannot hold a CNAME record.
const apex = computed(() => name.value === '@')

function close() {
  if (!props.verifying) emit('close')
}

// Focus the dialog when it opens so Escape closes it.
watch(
  () => props.domain,
  async (domain) => {
    if (!domain) return
    await nextTick()
    dialog.value?.focus()
  },
)
</script>

<template>
  <div
    v-if="domain"
    class="fixed inset-0 z-[150] flex items-center justify-center overflow-y-auto bg-black/40 backdrop-blur-sm px-4 py-6"
    data-test="dns-modal"
    @click.self="close"
    @keydown.esc="close"
  >
    <div
      ref="dialog"
      role="dialog"
      aria-modal="true"
      aria-labelledby="dns-modal-title"
      tabindex="-1"
      class="w-full max-w-lg bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-sm p-5 focus:outline-none"
    >
      <h2 id="dns-modal-title" class="text-sm font-bold text-gray-900 dark:text-zinc-100">
        Point <span class="font-mono">{{ domain.hostname }}</span> at this folder
      </h2>
      <p class="mt-1.5 text-xs text-gray-500 dark:text-zinc-400">
        Add one DNS record where <span class="font-mono">{{ zone }}</span> is managed, usually the company you bought it from
        (Cloudflare, GoDaddy, Namecheap, Squarespace, Route 53…). Open its DNS settings and add:
      </p>

      <p
        v-if="apex"
        role="alert"
        class="mt-3 rounded-lg border border-rose-200 dark:border-rose-500/30 bg-rose-50 dark:bg-rose-500/10 px-3 py-2 text-xs font-medium text-rose-700 dark:text-rose-300"
        data-test="dns-apex"
      >
        {{ domain.hostname }} is a bare domain, which can't have a CNAME record. Remove it and add a subdomain such as
        www.{{ domain.hostname }} instead.
      </p>

      <dl v-else class="mt-3 divide-y divide-gray-100 dark:divide-zinc-800 rounded-lg border border-gray-100 dark:border-zinc-800 text-xs" data-test="dns-record">
        <div class="flex items-center gap-3 px-3 py-2">
          <dt class="w-24 shrink-0 text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Type</dt>
          <dd class="grow font-mono font-bold text-gray-900 dark:text-zinc-100">CNAME</dd>
        </div>
        <div class="flex items-center gap-3 px-3 py-2">
          <dt class="w-24 shrink-0 text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Name / Host</dt>
          <dd class="grow min-w-0 truncate font-mono font-bold text-gray-900 dark:text-zinc-100" data-test="dns-name">{{ name }}</dd>
          <button type="button" :class="ICON_BUTTON" title="Copy name" data-test="dns-copy-name" @click="copy(name, 'Name copied')">
            <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
              <path stroke-linecap="round" stroke-linejoin="round" :d="COPY_ICON" />
            </svg>
          </button>
        </div>
        <div class="flex items-center gap-3 px-3 py-2">
          <dt class="w-24 shrink-0 text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Target</dt>
          <dd class="grow min-w-0 truncate font-mono font-bold text-gray-900 dark:text-zinc-100" data-test="dns-target">{{ domain.target }}</dd>
          <button type="button" :class="ICON_BUTTON" title="Copy target" data-test="dns-copy-target" @click="copy(domain.target, 'Target copied')">
            <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
              <path stroke-linecap="round" stroke-linejoin="round" :d="COPY_ICON" />
            </svg>
          </button>
        </div>
        <div class="flex items-center gap-3 px-3 py-2">
          <dt class="w-24 shrink-0 text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">TTL</dt>
          <dd class="grow text-gray-900 dark:text-zinc-100">Auto, or 3600 (1 hour)</dd>
        </div>
      </dl>

      <ul v-if="!apex" class="mt-3 space-y-1.5 text-xs text-gray-500 dark:text-zinc-400 list-disc pl-4" data-test="dns-tips">
        <li>
          The target is also called <em>Value</em>, <em>Points to</em> or <em>Alias</em>. Some providers want the full name,
          <span class="font-mono">{{ domain.hostname }}</span>, in the Name field instead of <span class="font-mono">{{ name }}</span>.
        </li>
        <li>Delete any other A, AAAA or CNAME record named <span class="font-mono">{{ name }}</span> first: a name with a CNAME can't have other records.</li>
        <li>
          On Cloudflare, set <em>Proxy status</em> to <strong class="text-gray-900 dark:text-zinc-100">DNS only</strong> (grey cloud). A proxied
          record hides the CNAME, so the domain can't be verified or get its certificate.
        </li>
        <li>
          New records usually show up within minutes, rarely up to 48 hours. We check pending domains every few minutes for 7 days,
          or right away with Verify now. Once verified, HTTPS is set up automatically.
        </li>
      </ul>

      <p v-if="pricing" class="mt-3 text-[11px] text-gray-400 dark:text-zinc-500" data-test="dns-pricing">
        Nothing is charged until the record is verified. Then it costs {{ pricing.domainSetupCredits }} credits to set up and
        {{ pricing.domainCredits }} a month.
      </p>

      <div class="mt-5 flex justify-end gap-2">
        <button type="button" :disabled="verifying" :class="SECONDARY" data-test="dns-close" @click="close">Close</button>
        <button
          v-if="!apex"
          type="button"
          :disabled="verifying"
          :class="PRIMARY"
          data-test="dns-verify"
          @click="emit('verify')"
        >
          {{ verifying ? 'Checking…' : 'Verify now' }}
        </button>
      </div>
    </div>
  </div>
</template>
