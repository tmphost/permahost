<!--
  Copyright 2026 Contextual, Inc. https://tmphost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import BrandLogo from '../components/BrandLogo.vue'
import { fetchAuthConfig, providerLoginUrl } from '../api'
import { useToasts } from '../composables/useToasts'
import { loginErrorMessage } from '../utils/loginErrors'
import { svgDataUri } from '../utils/providers'

const route = useRoute()
const { notifyError } = useToasts()

const loading = ref(true)
const providers = ref([])
const configError = ref('')

const redirect = computed(() => (typeof route.query.redirect === 'string' ? route.query.redirect : ''))
const errorMessage = computed(() => loginErrorMessage(route.query.error))
const buttons = computed(() =>
  providers.value.map((p) => ({
    id: p.id,
    label: `Continue with ${p.name}`,
    icon: p.icon ? svgDataUri(p.icon) : '',
    href: providerLoginUrl(p.id, redirect.value),
  })),
)

onMounted(async () => {
  try {
    providers.value = await fetchAuthConfig()
  } catch (err) {
    configError.value = 'Sign-in is unavailable right now.'
    notifyError(err.message)
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div class="min-h-[100dvh] flex items-center justify-center bg-zinc-50 dark:bg-zinc-950 px-4 py-10">
    <div class="w-full max-w-sm bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-sm p-8">
      <div class="text-center mb-8">
        <BrandLogo tile class="w-12 h-12 mx-auto mb-5" />
        <h1 class="text-2xl font-black text-gray-900 dark:text-zinc-100 tracking-tight">tmphost.io</h1>
        <p class="mt-1.5 text-sm text-gray-500 dark:text-zinc-400">Sign in to continue.</p>
      </div>

      <p
        v-if="errorMessage"
        role="alert"
        class="mb-4 rounded-lg border border-rose-100 dark:border-rose-500/20 bg-rose-50 dark:bg-rose-500/10 px-3 py-2 text-xs font-medium text-rose-600 dark:text-rose-400"
        data-test="login-error"
      >
        {{ errorMessage }}
      </p>

      <div v-if="loading" class="flex justify-center py-6" data-test="login-loading">
        <svg class="animate-spin h-5 w-5 text-gray-400 dark:text-zinc-500" fill="none" viewBox="0 0 24 24">
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
          <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
        </svg>
      </div>

      <p v-else-if="configError" class="text-center text-sm text-gray-500 dark:text-zinc-400" data-test="login-config-error">
        {{ configError }}
      </p>

      <p v-else-if="buttons.length === 0" class="text-center text-sm text-gray-500 dark:text-zinc-400" data-test="login-no-providers">
        No sign-in providers are configured.
      </p>

      <div v-else class="space-y-3">
        <a
          v-for="button in buttons"
          :key="button.id"
          :href="button.href"
          :data-test="`login-${button.id}`"
          class="w-full flex items-center justify-center gap-3 px-4 py-3 rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100 hover:border-gray-300 dark:hover:border-zinc-600 hover:bg-zinc-50 dark:hover:bg-zinc-950 transition-colors"
        >
          <!-- Google and GitHub ship their icons here; custom providers send an SVG. -->
          <svg v-if="button.id === 'google'" class="w-4 h-4" viewBox="0 0 24 24" aria-hidden="true">
            <path d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z" fill="#4285F4" />
            <path d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" fill="#34A853" />
            <path d="M5.84 14.1c-.22-.66-.35-1.36-.35-2.1s.13-1.44.35-2.1V7.06H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.94l3.66-2.84z" fill="#FBBC05" />
            <path d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.06l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z" fill="#EA4335" />
          </svg>
          <svg v-else-if="button.id === 'github'" class="w-4 h-4 text-gray-900" fill="currentColor" viewBox="0 0 24 24" aria-hidden="true">
            <path d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0112 6.844c.85.004 1.705.115 2.504.337 1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.019 10.019 0 0022 12.017C22 6.484 17.522 2 12 2z" />
          </svg>
          <img v-else-if="button.icon" :src="button.icon" alt="" class="w-4 h-4" data-test="login-icon" />
          {{ button.label }}
        </a>
        <p class="pt-2 text-center text-[11px] text-gray-400 dark:text-zinc-500">
          New accounts get <span class="font-bold text-gray-500 dark:text-zinc-400">250 free operations</span> every month.
        </p>
      </div>

      <p class="mt-6 text-center text-xs text-gray-500 dark:text-zinc-400">
        No account?
        <router-link to="/upload" class="font-bold text-gray-900 dark:text-zinc-100 hover:underline" data-test="login-upload">
          Upload without signing in
        </router-link>
      </p>

      <div class="mt-8 pt-5 border-t border-gray-100 dark:border-zinc-800 flex items-center justify-between text-[11px] font-bold text-gray-400 dark:text-zinc-500">
        <span>&copy; {{ new Date().getFullYear() }} tmphost.io</span>
        <router-link to="/setup" class="hover:text-gray-900 dark:hover:text-zinc-100 transition-colors" data-test="login-setup">Setup</router-link>
      </div>
    </div>
  </div>
</template>
