// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it, vi } from 'vitest'
import { createMemoryHistory } from 'vue-router'

vi.mock('../src/api', () => ({
  fetchSettings: vi.fn(async () => null),
  fetchUser: vi.fn(async () => ({ id: 'u1', name: 'Ada', email: 'ada@x', credits: 1, creditsLimit: 250 })),
  fetchAuthConfig: vi.fn(async () => []),
  providerLoginUrl: vi.fn(() => '#'),
  logout: vi.fn(),
  BASE_PATH: '',
  fetchLimits: vi.fn(),
  listFolders: vi.fn(),
  createFolder: vi.fn(),
  fetchFolder: vi.fn(),
  deleteFolder: vi.fn(),
  uploadFile: vi.fn(),
  deleteFile: vi.fn(),
}))

import { clickOutside, createAuthGuard, createtmphostApp, routes } from '../src/app'
import { usePlatformStore } from '../src/stores/platformStore'

describe('routes', () => {
  it('only /login, /upload and /setup are public and every view is lazy', () => {
    expect(routes.filter((r) => r.meta?.public).map((r) => r.path)).toEqual(['/login', '/upload', '/setup'])
    for (const r of routes.filter((r) => r.component)) {
      expect(typeof r.component).toBe('function')
    }
  })

  it('lazy components resolve', async () => {
    for (const r of routes.filter((r) => r.component)) {
      expect((await r.component()).default).toBeTruthy()
    }
  })
})

describe('createAuthGuard', () => {
  const login = { meta: { public: true, guestOnly: true }, fullPath: '/login' }
  const home = { meta: {}, fullPath: '/' }
  const deep = { meta: {}, fullPath: '/x?y=1' }

  it('lets guests reach public pages and sends users home', async () => {
    expect(await createAuthGuard(async () => null)(login)).toBe(true)
    expect(await createAuthGuard(async () => ({ id: 1 }))(login)).toBe('/')
    expect(await createAuthGuard(async () => ({ id: 1 }))({ meta: { public: true }, fullPath: '/p' })).toBe(true)
  })

  it('requires a user for private pages', async () => {
    expect(await createAuthGuard(async () => ({ id: 1 }))(home)).toBe(true)
    expect(await createAuthGuard(async () => null)(home)).toBe('/login')
    expect(await createAuthGuard(async () => null)(deep)).toEqual({ path: '/login', query: { redirect: '/x?y=1' } })
  })

  it('treats a failed lookup as signed out', async () => {
    const guard = createAuthGuard(async () => {
      throw new Error('down')
    })
    expect(await guard(home)).toBe('/login')
    expect(await guard(login)).toBe(true)
  })
})

describe('clickOutside', () => {
  it('fires only for clicks outside the element', () => {
    const el = document.createElement('div')
    const inner = document.createElement('span')
    el.append(inner)
    document.body.append(el)
    const handler = vi.fn()
    clickOutside.mounted(el, { value: handler })

    inner.click()
    expect(handler).not.toHaveBeenCalled()
    document.body.click()
    expect(handler).toHaveBeenCalledTimes(1)

    clickOutside.unmounted(el)
    document.body.click()
    expect(handler).toHaveBeenCalledTimes(1)
    el.remove()
  })
})

describe('createtmphostApp', () => {
  it('wires router, pinia, platform and titles', async () => {
    const { app, router, pinia } = createtmphostApp({ history: createMemoryHistory(), platform: 'desktop' })
    expect(usePlatformStore(pinia).isDesktop).toBe(true)
    expect(app._context.directives['click-outside']).toBe(clickOutside)

    await router.push('/')
    expect(router.currentRoute.value.name).toBe('files')
    expect(document.title).toBe('Files · tmphost.io')
    await router.push('/connect')
    expect(router.currentRoute.value.name).toBe('connect')
    expect(document.title).toBe('Connect · tmphost.io')

    await router.push('/nowhere')
    expect(router.currentRoute.value.path).toBe('/')

    router.addRoute({ path: '/untitled', component: { render: () => null } })
    await router.push('/untitled')
    expect(document.title).toBe('tmphost.io')
  })

  it('defaults to the web platform', () => {
    const { pinia } = createtmphostApp({ history: createMemoryHistory() })
    expect(usePlatformStore(pinia).isWeb).toBe(true)
  })

  it('applies the saved theme', () => {
    localStorage.setItem('theme', 'dark')
    createtmphostApp({ history: createMemoryHistory() })
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    localStorage.clear()
    document.documentElement.classList.remove('dark')
  })

  it('requires a history', () => {
    expect(() => createtmphostApp()).toThrow()
  })
})
