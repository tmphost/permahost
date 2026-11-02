// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { defineStore } from 'pinia'

// The localStorage key. index.html reads it too, to paint the right theme
// before the app loads.
export const THEME_KEY = 'theme'
export const THEMES = ['light', 'dark', 'system']

// The browser chrome colour (theme-color) per theme: the page background.
const THEME_COLOR = { light: '#fafafa', dark: '#09090b' }

const DARK_QUERY = '(prefers-color-scheme: dark)'

function storedTheme() {
  try {
    const theme = localStorage.getItem(THEME_KEY)
    return THEMES.includes(theme) ? theme : 'system'
  } catch {
    // Storage can be blocked (private windows, disabled site data).
    return 'system'
  }
}

function systemQuery() {
  return typeof window.matchMedia === 'function' ? window.matchMedia(DARK_QUERY) : null
}

// isDark is read fresh each time: the media query is not reactive, so a
// getter would keep the system's answer from when it was first read.
function isDark(theme) {
  if (theme === 'system') return Boolean(systemQuery()?.matches)
  return theme === 'dark'
}

// Light, dark, or whatever the system uses. Dark mode is the `dark` class on
// <html>, which the `dark:` variants in style.css key off.
export const useThemeStore = defineStore('theme', {
  state: () => ({ theme: storedTheme() }),
  actions: {
    setTheme(next) {
      this.theme = THEMES.includes(next) ? next : 'system'
      try {
        localStorage.setItem(THEME_KEY, this.theme)
      } catch {
        // Not remembered, but still applied to this page.
      }
      this.applyTheme()
    },
    applyTheme() {
      const dark = isDark(this.theme)
      document.documentElement.classList.toggle('dark', dark)
      document.querySelector('meta[name="theme-color"]')?.setAttribute('content', dark ? THEME_COLOR.dark : THEME_COLOR.light)
    },
    // init applies the theme and follows the system while the theme is
    // 'system'.
    init() {
      this.applyTheme()
      systemQuery()?.addEventListener('change', () => {
        if (this.theme === 'system') this.applyTheme()
      })
    },
  },
})
