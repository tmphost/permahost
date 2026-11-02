<!--
  Copyright 2026 Contextual, Inc. https://tmphost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
import { computed, ref } from 'vue'
import BrandLogo from './BrandLogo.vue'
import { useThemeStore } from '../stores/themeStore'
import { creditsPercent, renewsIn } from '../utils/credits'

const props = defineProps({
  user: { type: Object, default: null },
  collapsed: { type: Boolean, default: false },
  mobileOpen: { type: Boolean, default: false },
})
const emit = defineEmits(['toggle-collapse', 'logout'])

const NAV = [
  {
    section: 'Overview',
    items: [
      { to: '/', label: 'Files', icon: 'M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7z' },
      {
        to: '/connect',
        label: 'Connect',
        icon: 'M13.19 8.688a4.5 4.5 0 0 1 1.242 7.244l-4.5 4.5a4.5 4.5 0 0 1-6.364-6.364l1.757-1.757m13.35-.622 1.757-1.757a4.5 4.5 0 0 0-6.364-6.364l-4.5 4.5a4.5 4.5 0 0 0 1.242 7.244',
      },
    ],
  },
]

const THEME_OPTIONS = [
  {
    value: 'light',
    label: 'Light',
    icon: 'M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364-.707-.707M6.343 6.343l-.707-.707m12.728 0-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0z',
  },
  {
    value: 'dark',
    label: 'Dark',
    icon: 'M20.354 15.354A9 9 0 0 1 8.646 3.646 9.003 9.003 0 0 0 12 21a9.003 9.003 0 0 0 8.354-5.646z',
  },
  {
    value: 'system',
    label: 'System',
    icon: 'M9.75 17 9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 0 0 2-2V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v10a2 2 0 0 0 2 2z',
  },
]

const themeStore = useThemeStore()
const isUserMenuOpen = ref(false)

// The mobile drawer always shows labels, whatever the desktop state.
const compact = computed(() => props.collapsed && !props.mobileOpen)
const percent = computed(() => creditsPercent(props.user))
const renewal = computed(() => renewsIn(props.user?.creditsRenewAt))
const displayName = computed(() => props.user?.name || props.user?.email || 'Account')
const initial = computed(() => displayName.value.charAt(0).toUpperCase())

function closeUserMenu() {
  isUserMenuOpen.value = false
}

function logout() {
  closeUserMenu()
  emit('logout')
}
</script>

<template>
  <nav
    :class="[
      mobileOpen ? 'flex w-[280px] bg-white dark:bg-zinc-900 border-r border-gray-200 dark:border-zinc-800' : 'hidden',
      'md:flex md:relative md:bg-zinc-50 dark:md:bg-zinc-950 md:border-0 fixed inset-y-0 left-0 z-[100] shrink-0 flex-col h-full text-gray-900 dark:text-zinc-100 transition-[width] duration-200',
      compact ? 'md:w-16' : 'md:w-64',
    ]"
    data-test="sidebar"
  >
    <div :class="['flex flex-col min-h-0 grow', compact ? 'px-2 py-4' : 'p-4']">
      <!-- Header -->
      <div :class="['flex', compact ? 'flex-col items-center gap-2' : 'items-center gap-1']">
        <router-link to="/" :class="['flex items-center gap-2.5 min-w-0 p-1', compact ? '' : 'grow']">
          <BrandLogo tile class="w-7 h-7" />
          <span v-if="!compact" class="text-sm font-bold truncate" data-test="sidebar-brand">tmphost.io</span>
        </router-link>
        <button
          type="button"
          class="hidden md:inline-flex items-center justify-center size-8 shrink-0 rounded-lg text-gray-400 dark:text-zinc-500 hover:text-gray-900 dark:hover:text-zinc-100 hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors"
          :title="collapsed ? 'Expand sidebar' : 'Collapse sidebar'"
          data-test="sidebar-toggle"
          @click="emit('toggle-collapse')"
        >
          <svg
            :class="['w-4 h-4 transition-transform', collapsed ? 'rotate-180' : '']"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
          >
            <path d="m15 18-6-6 6-6" />
          </svg>
        </button>
      </div>

      <!-- Navigation -->
      <div class="mt-6 flex-1 min-h-0 overflow-y-auto no-scrollbar space-y-0.5">
        <template v-for="group in NAV" :key="group.section">
          <p v-if="!compact" class="px-2 pb-1 text-[11px] font-medium text-gray-400 dark:text-zinc-500">{{ group.section }}</p>
          <router-link
            v-for="item in group.items"
            :key="item.to"
            :to="item.to"
            :title="compact ? item.label : undefined"
            :class="[
              'flex items-center gap-2.5 px-2 py-1.5 rounded-lg text-xs transition-colors',
              compact ? 'justify-center' : '',
              $route.path === item.to
                ? 'bg-gray-200/70 dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 font-semibold'
                : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-100 dark:hover:bg-zinc-800 hover:text-gray-900 dark:hover:text-zinc-100',
            ]"
          >
            <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
              <path stroke-linecap="round" stroke-linejoin="round" :d="item.icon" />
            </svg>
            <span v-if="!compact">{{ item.label }}</span>
          </router-link>
        </template>
      </div>

      <!-- Operation credits -->
      <div
        v-if="user && !compact"
        class="mb-3 rounded-xl border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-3"
        data-test="sidebar-credits"
      >
        <div class="flex items-baseline justify-between">
          <span class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Operations</span>
          <span class="text-xs font-bold tabular-nums text-gray-900 dark:text-zinc-100">
            {{ user.credits }}<span class="text-gray-400 dark:text-zinc-500 font-medium"> / {{ user.creditsLimit }}</span>
          </span>
        </div>
        <div class="mt-2 h-1 rounded-full bg-gray-100 dark:bg-zinc-800 overflow-hidden">
          <div class="h-full bg-gray-900 dark:bg-zinc-100 rounded-full" :style="{ width: `${percent}%` }" data-test="credits-bar"></div>
        </div>
        <p v-if="renewal" class="mt-1.5 text-[10px] text-gray-400 dark:text-zinc-500">{{ renewal }}</p>
      </div>

      <!-- Account -->
      <div v-click-outside="closeUserMenu" class="relative pt-3 border-t border-gray-200 dark:border-zinc-800">
        <div
          v-if="isUserMenuOpen"
          :class="[
            'absolute z-[110] bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl p-1.5 shadow-sm',
            compact ? 'left-full bottom-0 ml-2 min-w-[200px]' : 'bottom-full left-0 right-0 mb-2',
          ]"
          data-test="user-menu"
        >
          <div class="px-2.5 py-2 border-b border-gray-100 dark:border-zinc-800 mb-1">
            <p class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500">Account</p>
            <p class="mt-0.5 text-xs font-bold text-gray-900 dark:text-zinc-100 truncate">{{ displayName }}</p>
            <p v-if="user?.name && user?.email" class="text-[10px] text-gray-400 dark:text-zinc-500 truncate">{{ user.email }}</p>
          </div>
          <div class="px-2.5 py-2 border-b border-gray-100 dark:border-zinc-800 mb-1">
            <p id="theme-label" class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 mb-2">Theme</p>
            <div
              class="flex items-center gap-1 rounded-lg border border-gray-200 dark:border-zinc-800 bg-zinc-50 dark:bg-zinc-950 p-0.5"
              role="radiogroup"
              aria-labelledby="theme-label"
              data-test="theme-picker"
            >
              <button
                v-for="option in THEME_OPTIONS"
                :key="option.value"
                type="button"
                role="radio"
                :aria-checked="themeStore.theme === option.value"
                :title="option.label"
                :class="[
                  'flex-1 flex justify-center py-1.5 rounded-md transition-colors',
                  themeStore.theme === option.value
                    ? 'bg-white dark:bg-zinc-800 text-gray-900 dark:text-zinc-100'
                    : 'text-gray-400 dark:text-zinc-500 hover:text-gray-900 dark:hover:text-zinc-100',
                ]"
                :data-test="`theme-${option.value}`"
                @click="themeStore.setTheme(option.value)"
              >
                <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                  <path stroke-linecap="round" stroke-linejoin="round" :d="option.icon" />
                </svg>
              </button>
            </div>
          </div>
          <button
            type="button"
            class="w-full flex items-center gap-2.5 px-2.5 py-2 rounded-lg text-xs font-bold text-rose-500 hover:bg-rose-50 dark:hover:bg-rose-500/10 transition-colors"
            data-test="logout"
            @click="logout"
          >
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1" />
            </svg>
            Log out
          </button>
        </div>
        <button
          type="button"
          :class="['flex items-center gap-2.5 w-full py-1.5 rounded-lg transition-colors hover:bg-gray-100 dark:hover:bg-zinc-800', compact ? 'justify-center' : 'px-2']"
          data-test="user-menu-toggle"
          @click="isUserMenuOpen = !isUserMenuOpen"
        >
          <span class="w-8 h-8 shrink-0 rounded-full border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 overflow-hidden flex items-center justify-center text-xs font-bold text-gray-700 dark:text-zinc-300">
            <img v-if="user?.picture" :src="user.picture" alt="" class="w-full h-full object-cover" referrerpolicy="no-referrer" />
            <span v-else>{{ initial }}</span>
          </span>
          <span v-if="!compact" class="text-sm font-semibold text-gray-700 dark:text-zinc-300 truncate text-left">{{ displayName }}</span>
        </button>
      </div>
    </div>
  </nav>
</template>
