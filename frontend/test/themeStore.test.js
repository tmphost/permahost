// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { THEME_KEY, useThemeStore } from '../src/stores/themeStore'

// stubSystem makes the system prefer dark (or not) and returns a function that
// fires the media query's change event.
function stubSystem(dark) {
  const query = { matches: dark, listeners: [], addEventListener: (_, fn) => query.listeners.push(fn) }
  vi.stubGlobal('matchMedia', vi.fn(() => query))
  return (next) => {
    query.matches = next
    query.listeners.forEach((fn) => fn())
  }
}

function themeColor() {
  return document.querySelector('meta[name="theme-color"]').getAttribute('content')
}

describe('themeStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
    document.documentElement.classList.remove('dark')
    document.head.innerHTML = '<meta name="theme-color" content="#fafafa">'
  })

  afterEach(() => vi.unstubAllGlobals())

  it('defaults to system and follows it', () => {
    const change = stubSystem(true)
    const store = useThemeStore()
    expect(store.theme).toBe('system')
    store.init()
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(themeColor()).toBe('#09090b')

    change(false)
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(themeColor()).toBe('#fafafa')
  })

  it('keeps an explicit choice when the system changes', () => {
    const change = stubSystem(false)
    const store = useThemeStore()
    store.init()
    store.setTheme('dark')
    expect(localStorage.getItem(THEME_KEY)).toBe('dark')
    change(false)
    expect(document.documentElement.classList.contains('dark')).toBe(true)

    store.setTheme('light')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })

  it('restores the saved theme and ignores unknown ones', () => {
    localStorage.setItem(THEME_KEY, 'dark')
    expect(useThemeStore().theme).toBe('dark')

    setActivePinia(createPinia())
    localStorage.setItem(THEME_KEY, 'neon')
    const store = useThemeStore()
    expect(store.theme).toBe('system')
    store.setTheme('neon')
    expect(store.theme).toBe('system')
  })

  it('treats a browser without matchMedia as light', () => {
    vi.stubGlobal('matchMedia', undefined)
    const store = useThemeStore()
    store.init()
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })

  it('works without storage or a theme-color tag', () => {
    stubSystem(false)
    document.head.innerHTML = ''
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('blocked')
    })
    const store = useThemeStore()
    expect(store.theme).toBe('system')
    store.setTheme('dark')
    expect(store.theme).toBe('dark')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })
})
