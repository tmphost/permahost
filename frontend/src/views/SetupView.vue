<!--
  Copyright 2026 Contextual, Inc. https://permahost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
import { computed, onMounted, ref } from 'vue'
import AdminOverview from '../components/AdminOverview.vue'
import AdminUserDetail from '../components/AdminUserDetail.vue'
import AdminUsers from '../components/AdminUsers.vue'
import BrandLogo from '../components/BrandLogo.vue'
import { fetchSettings, saveSettings, setupLogin, setupLogout } from '../api'
import { useToasts } from '../composables/useToasts'
import { formatMinutes } from '../utils/files'
import { providerSlug, svgDataUri } from '../utils/providers'

const PG_DSN_EXAMPLE = 'postgres://user:password@host:5432/permahost?sslmode=disable'

const INPUT =
  'w-full rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-3 py-2 text-sm text-gray-900 dark:text-zinc-100 placeholder:text-gray-400 dark:placeholder:text-zinc-500 focus:border-gray-400 dark:focus:border-zinc-500 focus:outline-none'
const LABEL = 'block text-[11px] font-bold text-gray-500 dark:text-zinc-400 mb-1'
const TEXTAREA = `${INPUT} font-mono text-xs`
const PRIMARY =
  'rounded-lg bg-black dark:bg-white px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-white dark:text-zinc-900 hover:bg-gray-800 dark:hover:bg-zinc-200 disabled:opacity-50 transition-colors'
const SECONDARY =
  'rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100 hover:border-gray-300 dark:hover:border-zinc-600 transition-colors'

// The setup menu: one section at a time. Overview and Users manage accounts;
// every other section edits the server settings, which one Save stores
// together.
const SECTIONS = [
  { id: 'overview', label: 'Overview', icon: 'M3 3v18h18M7 15l4-4 3 3 5-6' },
  {
    id: 'users',
    label: 'Users',
    icon: 'M15 19.128a9.38 9.38 0 0 0 2.625.372 9.337 9.337 0 0 0 4.121-.952 4.125 4.125 0 0 0-7.533-2.493M15 19.128v-.003c0-1.113-.285-2.16-.786-3.07M15 19.128v.106A12.318 12.318 0 0 1 8.624 21c-2.331 0-4.512-.645-6.374-1.766l-.001-.109a6.375 6.375 0 0 1 11.964-3.07M12 6.375a3.375 3.375 0 1 1-6.75 0 3.375 3.375 0 0 1 6.75 0zm8.25 2.25a2.625 2.625 0 1 1-5.25 0 2.625 2.625 0 0 1 5.25 0z',
  },
  {
    id: 'domain',
    label: 'Domains',
    icon: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zm0 0c2.5-2.5 3.5-5.5 3.5-9S14.5 5.5 12 3m0 18c-2.5-2.5-3.5-5.5-3.5-9S9.5 5.5 12 3M3.5 9h17M3.5 15h17',
  },
  {
    id: 'tls',
    label: 'TLS',
    icon: 'M16.5 10.5V7a4.5 4.5 0 1 0-9 0v3.5M6.75 21h10.5a2.25 2.25 0 0 0 2.25-2.25v-6a2.25 2.25 0 0 0-2.25-2.25H6.75a2.25 2.25 0 0 0-2.25 2.25v6A2.25 2.25 0 0 0 6.75 21z',
  },
  {
    id: 'credits',
    label: 'Credits',
    icon: 'M12 6v12m-3-2.818.879.659c1.171.879 3.07.879 4.242 0 1.172-.879 1.172-2.303 0-3.182C13.536 12.219 12.768 12 12 12c-.725 0-1.45-.22-2.003-.659-1.106-.879-1.106-2.303 0-3.182s2.9-.879 4.006 0l.415.33M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0z',
  },
  {
    id: 'database',
    label: 'Database',
    icon: 'M4 6c0-1.66 3.58-3 8-3s8 1.34 8 3-3.58 3-8 3-8-1.34-8-3zm0 0v12c0 1.66 3.58 3 8 3s8-1.34 8-3V6M4 12c0 1.66 3.58 3 8 3s8-1.34 8-3',
  },
  {
    id: 'storage',
    label: 'Storage',
    icon: 'M20.25 7.5l-.625 10.632a2.25 2.25 0 0 1-2.247 2.118H6.622a2.25 2.25 0 0 1-2.247-2.118L3.75 7.5M10 11.25h4M3.375 7.5h17.25c.621 0 1.125-.504 1.125-1.125v-1.5c0-.621-.504-1.125-1.125-1.125H3.375c-.621 0-1.125.504-1.125 1.125v1.5c0 .621.504 1.125 1.125 1.125z',
  },
  { id: 'files', label: 'Files', icon: 'M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7z' },
  {
    id: 'signin',
    label: 'Sign-in',
    icon: 'M15.75 5.25a3 3 0 0 1 3 3m3 0a6 6 0 0 1-7.029 5.912c-.563-.097-1.159.026-1.563.43L10.5 17.25H8.25v2.25H6v2.25H2.25v-2.818c0-.597.237-1.17.659-1.591l6.499-6.499c.404-.404.527-1 .43-1.563A6 6 0 1 1 21.75 8.25z',
  },
]
// ACCOUNT_SECTIONS are the sections that don't edit the server settings.
const ACCOUNT_SECTIONS = ['overview', 'users']

const { notifyError, notifySuccess } = useToasts()

// 'loading' | 'login' | 'settings'
const phase = ref('loading')
const section = ref(SECTIONS[0].id)
// The user whose page is open in the users section; empty for the list.
const userId = ref('')
const editingSettings = computed(() => !ACCOUNT_SECTIONS.includes(section.value))
const token = ref('')
const loginError = ref('')
const busy = ref(false)

const form = ref(null)
const secretsSet = ref({})
const restartRequired = ref(false)
const saveError = ref('')

function load(state) {
  // Work on a copy; secrets always arrive blank.
  form.value = JSON.parse(JSON.stringify(state.settings))
  form.value.auth.providers = form.value.auth.providers || []
  secretsSet.value = state.secretsSet
  restartRequired.value = state.restartRequired
  phase.value = 'settings'
}

async function refresh() {
  try {
    const state = await fetchSettings()
    if (state) load(state)
    else phase.value = 'login'
  } catch (err) {
    notifyError(err.message)
    phase.value = 'login'
  }
}

async function login() {
  busy.value = true
  loginError.value = ''
  try {
    await setupLogin(token.value)
    token.value = ''
    await refresh()
  } catch (err) {
    loginError.value = err.message
  } finally {
    busy.value = false
  }
}

async function logout() {
  try {
    await setupLogout()
  } catch {
    // The cookie expires on its own; forget the session here regardless.
  }
  form.value = null
  openSection(SECTIONS[0].id)
  phase.value = 'login'
}

// expire returns to the sign-in form once the root session has ended.
function expire() {
  phase.value = 'login'
  loginError.value = 'Your setup session expired. Sign in again.'
}

// openSection shows a section; the users section starts at the list.
function openSection(id) {
  section.value = id
  userId.value = ''
}

// onError signs in again when the root session is gone, else reports the error.
function onError(err) {
  if (err.status === 401) {
    expire()
    return
  }
  notifyError(err.message)
}

async function save() {
  busy.value = true
  saveError.value = ''
  try {
    load(await saveSettings(form.value))
    notifySuccess('Settings saved')
  } catch (err) {
    if (err.status === 401) {
      expire()
      return
    }
    saveError.value = err.message
    notifyError(err.message)
  } finally {
    busy.value = false
  }
}

function addProvider() {
  form.value.auth.providers.push({ name: '', icon: '', clientId: '', clientSecret: '', authUrl: '' })
}

function removeProvider(index) {
  form.value.auth.providers.splice(index, 1)
}

async function uploadIcon(provider, event) {
  const file = event.target.files[0]
  if (!file) return
  provider.icon = await file.text()
  event.target.value = ''
}

function callbackUrl(provider) {
  return `${form.value.domain.appUrl.replace(/\/+$/, '')}/auth/${providerSlug(provider.name)}/callback`
}

function providerSecretSet(provider) {
  return Boolean(secretsSet.value.providerClientSecrets?.[providerSlug(provider.name)])
}

// ttlHint spells a minute count out in hours or days.
function ttlHint(minutes) {
  return Number.isInteger(minutes) && minutes > 0 ? `= ${formatMinutes(minutes)}` : 'Enter a whole number of minutes'
}

function secretPlaceholder(isSet) {
  return isSet ? '•••••••• (saved — leave blank to keep)' : 'Not set'
}

onMounted(refresh)
</script>

<template>
  <div class="min-h-[100dvh] bg-zinc-50 dark:bg-zinc-950 px-4 py-10">
    <div v-if="phase === 'loading'" class="flex justify-center py-24" data-test="setup-loading">
      <svg class="animate-spin h-5 w-5 text-gray-400 dark:text-zinc-500" fill="none" viewBox="0 0 24 24">
        <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
        <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
      </svg>
    </div>

    <!-- Root sign-in -->
    <div v-else-if="phase === 'login'" class="min-h-[80dvh] flex items-center justify-center">
      <form
        class="w-full max-w-sm bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-sm p-8"
        data-test="setup-login"
        @submit.prevent="login"
      >
        <div class="text-center mb-8">
          <BrandLogo tile class="w-12 h-12 mx-auto mb-5" />
          <h1 class="text-2xl font-black text-gray-900 dark:text-zinc-100 tracking-tight">Setup</h1>
          <p class="mt-1.5 text-sm text-gray-500 dark:text-zinc-400">Enter the root access token to configure permahost.io.</p>
        </div>
        <label for="root-token" :class="LABEL">Root access token</label>
        <input
          id="root-token"
          v-model="token"
          type="password"
          autocomplete="current-password"
          required
          :class="INPUT"
          data-test="setup-token"
        />
        <p v-if="loginError" role="alert" class="mt-3 text-xs font-medium text-rose-600 dark:text-rose-400" data-test="setup-login-error">
          {{ loginError }}
        </p>
        <button type="submit" :disabled="busy || !token" :class="[PRIMARY, 'mt-5 w-full']" data-test="setup-unlock">
          Unlock setup
        </button>
      </form>
    </div>

    <div v-else class="mx-auto w-full max-w-7xl" data-test="setup-page">
      <header class="flex flex-wrap items-center gap-3 mb-6">
        <BrandLogo tile class="w-9 h-9" />
        <div class="grow min-w-0">
          <h1 class="text-lg font-bold text-gray-900 dark:text-zinc-100 tracking-tight">Setup</h1>
          <p class="text-xs text-gray-500 dark:text-zinc-400">Accounts and server settings of permahost.io. Only root can see this page.</p>
        </div>
        <button type="button" :class="SECONDARY" data-test="setup-logout" @click="logout">Sign out</button>
        <button v-if="editingSettings" type="submit" form="setup-form" :disabled="busy" :class="PRIMARY" data-test="setup-save">Save</button>
      </header>

      <div class="flex flex-col md:flex-row gap-4 md:gap-8 min-w-0">
        <!-- Menu: a scrolling row above the section on phones, a column beside it from md up. -->
        <nav class="w-full md:w-48 shrink-0" aria-label="Setup">
          <ul class="flex md:flex-col gap-1 overflow-x-auto no-scrollbar">
            <li v-for="item in SECTIONS" :key="item.id" class="shrink-0" :class="{ 'md:mt-3 md:pt-3 md:border-t border-gray-200 dark:border-zinc-800': item.id === 'domain' }">
              <button
                type="button"
                :aria-current="section === item.id ? 'page' : undefined"
                :class="[
                  'w-full flex items-center gap-2.5 px-3 py-2 rounded-lg text-xs whitespace-nowrap transition-colors',
                  section === item.id
                    ? 'bg-gray-200/70 dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 font-semibold'
                    : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-100 dark:hover:bg-zinc-800 hover:text-gray-900 dark:hover:text-zinc-100',
                ]"
                :data-test="`setup-tab-${item.id}`"
                @click="openSection(item.id)"
              >
                <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
                  <path stroke-linecap="round" stroke-linejoin="round" :d="item.icon" />
                </svg>
                {{ item.label }}
              </button>
            </li>
          </ul>
        </nav>

        <div class="flex-1 min-w-0">
          <AdminOverview v-if="section === 'overview'" @error="onError" />
          <AdminUserDetail v-else-if="section === 'users' && userId" :id="userId" :key="userId" @back="userId = ''" @error="onError" />
          <AdminUsers v-else-if="section === 'users'" @open="userId = $event" @error="onError" />

          <!-- Settings: kept mounted while Overview or Users is open, so unsaved edits survive. -->
          <form v-show="editingSettings" id="setup-form" data-test="setup-form" @submit.prevent="save">

            <p
              v-if="restartRequired"
              class="mb-4 rounded-lg border border-amber-200 dark:border-amber-500/30 bg-amber-50 dark:bg-amber-500/10 px-3 py-2 text-xs font-medium text-amber-800 dark:text-amber-300"
              data-test="setup-restart"
            >
              Saved. Domain, TLS, database, storage and sign-in changes take effect after the server restarts.
            </p>
            <p
              v-if="saveError"
              role="alert"
              class="mb-4 rounded-lg border border-rose-100 dark:border-rose-500/20 bg-rose-50 dark:bg-rose-500/10 px-3 py-2 text-xs font-medium text-rose-600 dark:text-rose-400"
              data-test="setup-save-error"
            >
              {{ saveError }}
            </p>

            <div>
              <!-- Domains -->
              <section class="rounded-xl border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-5" v-show="section === 'domain'" data-test="setup-domain">
                <h2 class="text-sm font-bold text-gray-900 dark:text-zinc-100">Domains</h2>
                <p class="text-xs text-gray-500 dark:text-zinc-400 mb-4">Requires a restart.</p>
                <div class="space-y-3">
                  <div>
                    <label for="app-url" :class="LABEL">App URL</label>
                    <input id="app-url" v-model="form.domain.appUrl" placeholder="https://app.permahost.io" :class="INPUT" />
                    <p class="mt-1 text-[11px] text-gray-400 dark:text-zinc-500">
                      Default http://localhost:2027. Its host (any domain or subdomain) is where the app is served; OAuth callbacks use it.
                    </p>
                  </div>
                  <div>
                    <label for="sites-domain" :class="LABEL">Sites domain</label>
                    <input id="sites-domain" v-model="form.domain.sitesDomain" placeholder="permahost.io" :class="INPUT" />
                    <p class="mt-1 text-[11px] text-gray-400 dark:text-zinc-500">
                      Dynamic subdomains are served as f + 11 characters under it, e.g. f1a2b3c4d5e6.permahost.io. Leave blank to disable.
                    </p>
                  </div>
                </div>
              </section>

              <!-- TLS -->
              <section class="rounded-xl border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-5" v-show="section === 'tls'" data-test="setup-tls">
                <div class="flex items-start justify-between gap-4">
                  <div>
                    <h2 class="text-sm font-bold text-gray-900 dark:text-zinc-100">TLS</h2>
                    <p class="text-xs text-gray-500 dark:text-zinc-400 mb-4">
                      Serve HTTPS directly with Let's Encrypt certificates, obtained and renewed automatically. Let's Encrypt must
                      reach this server on port 443 for the app host (and each subdomain). Requires a restart.
                    </p>
                  </div>
                  <label class="flex items-center gap-2 text-xs font-bold text-gray-700 dark:text-zinc-300">
                    <input v-model="form.tls.enabled" type="checkbox" class="accent-black dark:accent-white" data-test="setup-tls-enabled" />
                    Enabled
                  </label>
                </div>
                <div class="space-y-4" :class="{ 'opacity-50': !form.tls.enabled }">
                  <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
                    <div>
                      <label for="tls-port" :class="LABEL">HTTPS port</label>
                      <input id="tls-port" v-model.number="form.tls.port" type="number" min="1" max="65535" :class="[INPUT, 'max-w-[12rem]']" />
                    </div>
                    <div>
                      <label for="tls-email" :class="LABEL">Let's Encrypt email</label>
                      <input id="tls-email" v-model="form.tls.email" type="email" placeholder="ops@permahost.io" :class="INPUT" />
                      <p class="mt-1 text-[11px] text-gray-400 dark:text-zinc-500">Optional. Let's Encrypt sends expiry notices here.</p>
                    </div>
                  </div>

                  <div class="rounded-lg border border-gray-100 dark:border-zinc-800 p-4">
                    <div class="flex items-start justify-between gap-4">
                      <div>
                        <h3 class="text-xs font-bold text-gray-900 dark:text-zinc-100">Dynamic subdomains (lego)</h3>
                        <p class="text-[11px] text-gray-400 dark:text-zinc-500 mb-3">
                          Blank: each subdomain gets its own certificate on its first visit (Let's Encrypt allows about 50 new ones per
                          week per domain). With a Cloudflare API token (Zone DNS edit): one wildcard
                          *.{{ form.domain.sitesDomain || 'sites domain' }} certificate via DNS-01, no weekly limit.
                        </p>
                      </div>
                      <label class="flex items-center gap-2 text-xs font-bold text-gray-700 dark:text-zinc-300">
                        <input v-model="form.tls.lego.enabled" type="checkbox" class="accent-black dark:accent-white" data-test="setup-lego-enabled" />
                        Enabled
                      </label>
                    </div>
                    <div class="grid grid-cols-1 sm:grid-cols-2 gap-3" :class="{ 'opacity-50': !form.tls.lego.enabled }">
                      <div>
                        <label for="lego-token" :class="LABEL">Cloudflare API token (optional)</label>
                        <input
                          id="lego-token"
                          v-model="form.tls.lego.cloudflareApiToken"
                          type="password"
                          autocomplete="new-password"
                          :placeholder="secretsSet.cloudflareApiToken ? secretPlaceholder(true) : 'Optional'"
                          :class="INPUT"
                        />
                      </div>
                    </div>
                  </div>
                </div>
              </section>

              <!-- Credits -->
              <section class="rounded-xl border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-5" v-show="section === 'credits'" data-test="setup-credits">
                <h2 class="text-sm font-bold text-gray-900 dark:text-zinc-100">Credits</h2>
                <p class="text-xs text-gray-500 dark:text-zinc-400 mb-4">Applies immediately: new accounts and the daily 00:00 UTC renewal use it.</p>
                <label for="monthly-grant" :class="LABEL">Credits per account, every 30 days</label>
                <input
                  id="monthly-grant"
                  v-model.number="form.credits.monthlyGrant"
                  type="number"
                  min="0"
                  :class="[INPUT, 'max-w-[12rem]']"
                  data-test="setup-grant"
                />
                <p class="mt-1 text-[11px] text-gray-400 dark:text-zinc-500" data-test="setup-grant-hint">
                  To grant or deduct one account's credits, open
                  <button type="button" class="font-bold underline hover:text-gray-900 dark:hover:text-zinc-100" data-test="setup-grant-users" @click="openSection('users')">Users</button>.
                </p>
              </section>

              <!-- Database -->
              <section class="rounded-xl border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-5" v-show="section === 'database'" data-test="setup-database">
                <h2 class="text-sm font-bold text-gray-900 dark:text-zinc-100">Database</h2>
                <p class="text-xs text-gray-500 dark:text-zinc-400 mb-4">Requires a restart. Data is not copied between databases.</p>
                <div class="inline-flex rounded-lg border border-gray-200 dark:border-zinc-800 p-0.5 mb-4" role="radiogroup">
                  <button
                    v-for="option in [{ value: 'sqlite', label: 'SQLite' }, { value: 'postgres', label: 'PostgreSQL' }]"
                    :key="option.value"
                    type="button"
                    role="radio"
                    :aria-checked="form.database.driver === option.value"
                    :class="[
                      'rounded-md px-3 py-1.5 text-xs font-bold transition-colors',
                      form.database.driver === option.value ? 'bg-black dark:bg-white text-white dark:text-zinc-900' : 'text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-100',
                    ]"
                    :data-test="`setup-driver-${option.value}`"
                    @click="form.database.driver = option.value"
                  >
                    {{ option.label }}
                  </button>
                </div>

                <div v-if="form.database.driver === 'sqlite'" data-test="setup-sqlite">
                  <label for="sqlite-dsn" :class="LABEL">SQLite DSN</label>
                  <input id="sqlite-dsn" v-model="form.database.sqlite.dsn" :class="INPUT" />
                </div>
                <div v-else data-test="setup-postgres">
                  <label for="pg-dsn" :class="LABEL">PostgreSQL DSN</label>
                  <input
                    id="pg-dsn"
                    v-model="form.database.postgres.dsn"
                    type="password"
                    autocomplete="off"
                    spellcheck="false"
                    :placeholder="secretsSet.postgresDsn ? secretPlaceholder(true) : PG_DSN_EXAMPLE"
                    :class="INPUT"
                  />
                  <p class="mt-1 text-[11px] text-gray-400 dark:text-zinc-500">A postgres:// URL or key=value string. It holds the password, so it is never shown again.</p>
                </div>
              </section>

              <!-- Storage -->
              <section class="rounded-xl border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-5" v-show="section === 'storage'" data-test="setup-storage">
                <h2 class="text-sm font-bold text-gray-900 dark:text-zinc-100">Storage</h2>
                <p class="text-xs text-gray-500 dark:text-zinc-400 mb-4">Where uploaded files live. Requires a restart. Files are not copied between drivers.</p>
                <div class="inline-flex rounded-lg border border-gray-200 dark:border-zinc-800 p-0.5 mb-4" role="radiogroup">
                  <button
                    v-for="option in [{ value: 'local', label: 'Local disk' }, { value: 's3', label: 'S3' }]"
                    :key="option.value"
                    type="button"
                    role="radio"
                    :aria-checked="form.storage.driver === option.value"
                    :class="[
                      'rounded-md px-3 py-1.5 text-xs font-bold transition-colors',
                      form.storage.driver === option.value ? 'bg-black dark:bg-white text-white dark:text-zinc-900' : 'text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-100',
                    ]"
                    :data-test="`setup-storage-${option.value}`"
                    @click="form.storage.driver = option.value"
                  >
                    {{ option.label }}
                  </button>
                </div>

                <div v-if="form.storage.driver === 'local'" data-test="setup-local">
                  <label for="local-path" :class="LABEL">Local path</label>
                  <input id="local-path" v-model="form.storage.local.path" :class="INPUT" />
                </div>
                <div v-else class="grid grid-cols-1 sm:grid-cols-2 gap-3" data-test="setup-s3">
                  <p class="sm:col-span-2 text-[11px] text-gray-400 dark:text-zinc-500">Any S3-compatible bucket.</p>
                  <div class="sm:col-span-2">
                    <label for="s3-endpoint" :class="LABEL">Endpoint (blank for AWS)</label>
                    <input id="s3-endpoint" v-model="form.storage.s3.endpoint" placeholder="https://" :class="INPUT" />
                  </div>
                  <div>
                    <label for="s3-region" :class="LABEL">Region</label>
                    <input id="s3-region" v-model="form.storage.s3.region" :class="INPUT" />
                  </div>
                  <div>
                    <label for="s3-bucket" :class="LABEL">Bucket</label>
                    <input id="s3-bucket" v-model="form.storage.s3.bucket" :class="INPUT" />
                  </div>
                  <div>
                    <label for="s3-key" :class="LABEL">Access key ID</label>
                    <input id="s3-key" v-model="form.storage.s3.accessKeyId" :class="INPUT" />
                  </div>
                  <div>
                    <label for="s3-secret" :class="LABEL">Secret access key</label>
                    <input
                      id="s3-secret"
                      v-model="form.storage.s3.secretAccessKey"
                      type="password"
                      autocomplete="new-password"
                      :placeholder="secretPlaceholder(secretsSet.s3SecretAccessKey)"
                      :class="INPUT"
                    />
                  </div>
                  <label class="sm:col-span-2 flex items-center gap-2 text-xs text-gray-700 dark:text-zinc-300">
                    <input v-model="form.storage.s3.usePathStyle" type="checkbox" class="accent-black dark:accent-white" data-test="setup-s3-path-style" />
                    Use path-style URLs (MinIO and some providers)
                  </label>
                </div>
              </section>

              <!-- Files -->
              <section class="rounded-xl border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-5" v-show="section === 'files'" data-test="setup-files">
                <h2 class="text-sm font-bold text-gray-900 dark:text-zinc-100">Files</h2>
                <p class="text-xs text-gray-500 dark:text-zinc-400 mb-4" data-test="setup-files-help">
                  Limits for every upload and the form of public URLs. Account folders are permanent; only uploads without
                  sign-in expire. Uploads without sign-in, their max TTL and public URLs apply immediately; max upload size needs a
                  restart.
                </p>
                <label class="flex items-center gap-2 text-xs font-bold text-gray-700 dark:text-zinc-300 mb-1">
                  <input
                    :checked="!form.files.anonymousUploadsDisabled"
                    type="checkbox"
                    class="accent-black dark:accent-white"
                    data-test="setup-anonymous-uploads"
                    @change="form.files.anonymousUploadsDisabled = !$event.target.checked"
                  />
                  Allow uploads without an account
                </label>
                <p class="text-[11px] text-gray-400 dark:text-zinc-500 mb-4" data-test="setup-anonymous-uploads-hint">
                  {{
                    form.files.anonymousUploadsDisabled
                      ? 'Off: the public Upload page is closed and every upload needs an account.'
                      : 'Visitors can upload from the public Upload page, with lower limits, into folders that expire.'
                  }}
                </p>
                <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  <div>
                    <label for="max-upload" :class="LABEL">Max upload size (MB)</label>
                    <input
                      id="max-upload"
                      v-model.number="form.files.maxUploadMb"
                      type="number"
                      min="1"
                      :class="INPUT"
                      data-test="setup-max-upload"
                    />
                    <p class="mt-1 text-[11px] text-gray-400 dark:text-zinc-500">Per file upload.</p>
                  </div>
                  <div>
                    <label for="anonymous-max-ttl" :class="LABEL">Max TTL without sign-in (minutes)</label>
                    <input
                      id="anonymous-max-ttl"
                      v-model.number="form.files.anonymousMaxTtlMinutes"
                      type="number"
                      min="1"
                      max="10080"
                      :disabled="form.files.anonymousUploadsDisabled"
                      :class="[INPUT, 'disabled:opacity-50']"
                      data-test="setup-anonymous-max-ttl"
                    />
                    <p class="mt-1 text-[11px] text-gray-400 dark:text-zinc-500" data-test="setup-anonymous-max-ttl-hint">
                      {{ ttlHint(form.files.anonymousMaxTtlMinutes) }}, for uploads from the public Upload page. Default 10, at most 7 days.
                    </p>
                  </div>
                </div>

                <p id="public-urls-label" class="mt-5 text-[11px] font-bold text-gray-500 dark:text-zinc-400 mb-1">Public URLs</p>
                <div class="inline-flex rounded-lg border border-gray-200 dark:border-zinc-800 p-0.5 mb-2" role="radiogroup" aria-labelledby="public-urls-label">
                  <button
                    v-for="option in [{ value: false, label: 'Path' }, { value: true, label: 'Subdomains' }]"
                    :key="option.label"
                    type="button"
                    role="radio"
                    :aria-checked="Boolean(form.files.subdomains) === option.value"
                    :class="[
                      'rounded-md px-3 py-1.5 text-xs font-bold transition-colors',
                      Boolean(form.files.subdomains) === option.value ? 'bg-black dark:bg-white text-white dark:text-zinc-900' : 'text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-100',
                    ]"
                    :data-test="`setup-urls-${option.value ? 'subdomains' : 'path'}`"
                    @click="form.files.subdomains = option.value"
                  >
                    {{ option.label }}
                  </button>
                </div>
                <div v-if="form.files.subdomains" data-test="setup-subdomains">
                  <p class="text-[11px] text-gray-400 dark:text-zinc-500 mb-3" data-test="setup-subdomains-help">
                    Each folder is served at <code class="font-mono">f&lt;folderId&gt;.&lt;public domain&gt;</code>, where the
                    folder ID is 11 characters (e.g. f1a2b3c4d5e6.permahost.io). Needs a wildcard DNS record and certificate for the
                    domain. Leave the public domain blank to use the sites domain.
                  </p>
                  <label for="public-domain" :class="LABEL">Public domain</label>
                  <input
                    id="public-domain"
                    v-model.trim="form.files.publicDomain"
                    placeholder="permahost.io"
                    autocomplete="off"
                    spellcheck="false"
                    :class="INPUT"
                    data-test="setup-public-domain"
                  />
                </div>
                <p v-else class="text-[11px] text-gray-400 dark:text-zinc-500" data-test="setup-path">
                  Files are served at <code class="font-mono">&lt;base URL&gt;/files/&lt;userId&gt;/&lt;folderId&gt;/…</code>.
                </p>
              </section>

              <!-- Sign-in providers -->
              <section class="rounded-xl border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-5" v-show="section === 'signin'" data-test="setup-signin">
                <h2 class="text-sm font-bold text-gray-900 dark:text-zinc-100">Sign-in providers</h2>
                <p class="text-xs text-gray-500 dark:text-zinc-400 mb-4">Clear a client ID to disable that provider. Requires a restart.</p>
                <div class="space-y-4">
                  <div
                    v-for="provider in [
                      { key: 'google', label: 'Google', secretSet: secretsSet.googleClientSecret },
                      { key: 'github', label: 'GitHub', secretSet: secretsSet.githubClientSecret },
                    ]"
                    :key="provider.key"
                    class="grid grid-cols-1 sm:grid-cols-2 gap-3"
                    :data-test="`setup-provider-${provider.key}`"
                  >
                    <div>
                      <label :for="`${provider.key}-id`" :class="LABEL">{{ provider.label }} client ID</label>
                      <input :id="`${provider.key}-id`" v-model="form.auth[provider.key].clientId" :class="INPUT" />
                    </div>
                    <div>
                      <label :for="`${provider.key}-secret`" :class="LABEL">{{ provider.label }} client secret</label>
                      <input
                        :id="`${provider.key}-secret`"
                        v-model="form.auth[provider.key].clientSecret"
                        type="password"
                        autocomplete="new-password"
                        :placeholder="secretPlaceholder(provider.secretSet)"
                        :class="INPUT"
                      />
                    </div>
                  </div>
                </div>

                <div class="mt-6 border-t border-gray-100 dark:border-zinc-800 pt-5">
                  <div class="flex items-center justify-between gap-4 mb-3">
                    <div>
                      <h3 class="text-xs font-bold text-gray-900 dark:text-zinc-100">Custom providers</h3>
                      <p class="text-[11px] text-gray-400 dark:text-zinc-500">Any OpenID Connect provider (Okta, Auth0, Keycloak, ...).</p>
                    </div>
                    <button type="button" :class="SECONDARY" data-test="setup-add-provider" @click="addProvider">Add provider</button>
                  </div>
                  <div class="space-y-3">
                    <div
                      v-for="(provider, index) in form.auth.providers"
                      :key="index"
                      class="rounded-lg border border-gray-100 dark:border-zinc-800 p-4"
                      :data-test="`setup-custom-provider-${index}`"
                    >
                      <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
                        <div>
                          <label :for="`cp-${index}-name`" :class="LABEL">Name</label>
                          <input :id="`cp-${index}-name`" v-model="provider.name" placeholder="Okta" :class="INPUT" />
                        </div>
                        <div>
                          <label :for="`cp-${index}-auth-url`" :class="LABEL">Auth URL</label>
                          <input
                            :id="`cp-${index}-auth-url`"
                            v-model="provider.authUrl"
                            placeholder="https://accounts.example.com"
                            :class="INPUT"
                          />
                          <p class="mt-1 text-[11px] text-gray-400 dark:text-zinc-500">OpenID Connect issuer; endpoints come from /.well-known/openid-configuration</p>
                        </div>
                        <div>
                          <label :for="`cp-${index}-id`" :class="LABEL">Client ID</label>
                          <input :id="`cp-${index}-id`" v-model="provider.clientId" :class="INPUT" />
                        </div>
                        <div>
                          <label :for="`cp-${index}-secret`" :class="LABEL">Client secret</label>
                          <input
                            :id="`cp-${index}-secret`"
                            v-model="provider.clientSecret"
                            type="password"
                            autocomplete="new-password"
                            :placeholder="secretPlaceholder(providerSecretSet(provider))"
                            :class="INPUT"
                          />
                        </div>
                        <div class="sm:col-span-2">
                          <div class="flex items-center justify-between gap-3 mb-1">
                            <label :for="`cp-${index}-icon`" class="block text-[11px] font-bold text-gray-500">SVG icon</label>
                            <label class="cursor-pointer text-[11px] font-bold text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-100">
                              Upload .svg
                              <input
                                type="file"
                                accept=".svg,image/svg+xml"
                                class="sr-only"
                                :data-test="`cp-${index}-upload`"
                                @change="uploadIcon(provider, $event)"
                              />
                            </label>
                          </div>
                          <div class="flex items-start gap-3">
                            <textarea
                              :id="`cp-${index}-icon`"
                              v-model="provider.icon"
                              rows="3"
                              spellcheck="false"
                              placeholder="<svg xmlns=&quot;http://www.w3.org/2000/svg&quot; ...>"
                              :class="TEXTAREA"
                            />
                            <div class="shrink-0 w-10 h-10 rounded-lg border border-gray-100 dark:border-zinc-800 flex items-center justify-center">
                              <img
                                v-if="provider.icon"
                                :src="svgDataUri(provider.icon)"
                                alt=""
                                class="w-5 h-5"
                                :data-test="`cp-${index}-preview`"
                              />
                            </div>
                          </div>
                        </div>
                      </div>
                      <div class="mt-3 flex flex-wrap items-center justify-between gap-3">
                        <p class="min-w-0 text-[11px] text-gray-400 dark:text-zinc-500 break-all" :data-test="`cp-${index}-callback`">
                          Callback URL: <span class="font-mono text-gray-600 dark:text-zinc-400">{{ callbackUrl(provider) }}</span>
                        </p>
                        <button type="button" :class="SECONDARY" :data-test="`cp-${index}-remove`" @click="removeProvider(index)">
                          Remove
                        </button>
                      </div>
                    </div>
                  </div>
                </div>
              </section>
            </div>
          </form>
        </div>
      </div>
    </div>
  </div>
</template>
