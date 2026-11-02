<!--
  Copyright 2026 Contextual, Inc. https://tmphost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
import { computed, provide, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppSidebar from './components/AppSidebar.vue'
import ToastStack from './components/ToastStack.vue'
import { clearUserCache, fetchUser, logout } from './api'
import { useToasts } from './composables/useToasts'

const route = useRoute()
const router = useRouter()
const { notifyError } = useToasts()

const user = ref(null)
const isCollapsed = ref(false)
const isMobileMenuOpen = ref(false)

const isPublicPage = computed(() => Boolean(route.meta.public))

async function loadUser() {
  try {
    user.value = await fetchUser()
  } catch (err) {
    notifyError(err.message)
  }
}

// refreshUser refetches the user after the credits changed, so the sidebar
// counter follows uploads.
async function refreshUser() {
  clearUserCache()
  await loadUser()
}

provide('currentUser', { user, refreshUser })

async function onLogout() {
  try {
    await logout()
  } catch {
    // The session cookie may still be set, but the client forgets the user.
  }
  user.value = null
  router.push('/login')
}

watch(
  () => route.fullPath,
  () => {
    isMobileMenuOpen.value = false
    if (!isPublicPage.value && !user.value) loadUser()
  },
  { immediate: true },
)
</script>

<template>
  <div class="h-[100dvh] font-inter bg-zinc-50 dark:bg-zinc-950">
    <router-view v-if="isPublicPage" />

    <div v-else class="flex flex-col md:flex-row h-full overflow-hidden">
      <!-- Mobile "Menu" pill -->
      <button
        type="button"
        class="md:hidden fixed bottom-3 left-1/2 -translate-x-1/2 z-[60] flex items-center gap-2 px-4 py-1.5 rounded-full bg-black/80 dark:bg-white/90 backdrop-blur-md text-white dark:text-zinc-900 text-[10px] font-semibold active:scale-95 transition-transform"
        data-test="menu-pill"
        @click.stop="isMobileMenuOpen = true"
      >
        <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="round" stroke-linejoin="round" d="M4 6h16M4 12h16m-7 6h7" />
        </svg>
        Menu
      </button>
      <div
        v-if="isMobileMenuOpen"
        class="md:hidden fixed inset-0 z-[90] bg-black/40 backdrop-blur-sm"
        data-test="menu-backdrop"
        @click="isMobileMenuOpen = false"
      ></div>

      <AppSidebar
        :user="user"
        :collapsed="isCollapsed"
        :mobile-open="isMobileMenuOpen"
        @toggle-collapse="isCollapsed = !isCollapsed"
        @logout="onLogout"
      />

      <main class="grow min-w-0 h-full min-h-0 flex flex-col md:py-4 md:pr-4">
        <div class="h-full overflow-y-auto min-w-0 bg-white dark:bg-zinc-900 md:rounded-xl md:border border-gray-200 dark:border-zinc-800 no-scrollbar">
          <div class="px-2 pt-4 pb-6 md:p-4 h-full flex flex-col">
            <router-view v-slot="{ Component }">
              <component :is="Component" class="grow flex flex-col min-h-0 min-w-0" />
            </router-view>
          </div>
        </div>
      </main>
    </div>

    <ToastStack />
  </div>
</template>
