// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createPinia } from 'pinia'
import { inject } from 'vue'

vi.mock('../src/api', () => ({
  fetchUser: vi.fn(),
  clearUserCache: vi.fn(),
  logout: vi.fn(),
}))

import { clearUserCache, fetchUser, logout } from '../src/api'
import App from '../src/App.vue'
import { clickOutside } from '../src/app'
import { useToasts } from '../src/composables/useToasts'

const user = { id: 'u1', name: 'Ada', email: 'ada@x', credits: 250, creditsLimit: 250, creditsRenewAt: '2099-01-01T00:00:00Z' }

async function mountApp(path) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', component: { template: '<p data-test="login-page">login</p>' }, meta: { public: true } },
      { path: '/', component: { template: '<p data-test="home-page">home</p>' } },
      { path: '/other', component: { template: '<p>other</p>' } },
      { path: '/files', component: { template: '<p>files</p>' } },
      { path: '/connect', component: { template: '<p>connect</p>' } },
      {
        path: '/spend',
        component: {
          setup() {
            return inject('currentUser')
          },
          template: '<button data-test="spend" @click="refreshUser">{{ user?.credits }}</button>',
        },
      },
    ],
  })
  await router.push(path)
  const wrapper = mount(App, {
    attachTo: document.body,
    global: { plugins: [router, createPinia()], directives: { 'click-outside': clickOutside } },
  })
  await flushPromises()
  return { wrapper, router }
}

describe('App', () => {
  beforeEach(() => {
    fetchUser.mockResolvedValue(user)
    logout.mockResolvedValue()
  })
  afterEach(() => {
    useToasts().toasts.value = []
    document.body.innerHTML = ''
  })

  it('renders public pages without the shell', async () => {
    const { wrapper } = await mountApp('/login')
    expect(wrapper.find('[data-test="login-page"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="sidebar"]').exists()).toBe(false)
    expect(fetchUser).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('renders the shell with the user once', async () => {
    const { wrapper, router } = await mountApp('/')
    expect(wrapper.find('[data-test="home-page"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="sidebar"]').text()).toContain('Ada')
    await router.push('/other')
    await flushPromises()
    expect(fetchUser).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('toggles the sidebar and the mobile menu', async () => {
    const { wrapper, router } = await mountApp('/')
    await wrapper.find('[data-test="sidebar-toggle"]').trigger('click')
    expect(wrapper.find('[data-test="sidebar-brand"]').exists()).toBe(false)

    await wrapper.find('[data-test="menu-pill"]').trigger('click')
    expect(wrapper.find('[data-test="menu-backdrop"]').exists()).toBe(true)
    await wrapper.find('[data-test="menu-backdrop"]').trigger('click')
    expect(wrapper.find('[data-test="menu-backdrop"]').exists()).toBe(false)

    await wrapper.find('[data-test="menu-pill"]').trigger('click')
    await router.push('/other')
    await flushPromises()
    expect(wrapper.find('[data-test="menu-backdrop"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shares the user and refreshes the sidebar credits', async () => {
    const { wrapper } = await mountApp('/spend')
    expect(wrapper.find('[data-test="spend"]').text()).toBe('250')
    fetchUser.mockResolvedValue({ ...user, credits: 240 })
    await wrapper.find('[data-test="spend"]').trigger('click')
    await flushPromises()
    expect(clearUserCache).toHaveBeenCalled()
    expect(wrapper.find('[data-test="spend"]').text()).toBe('240')
    expect(wrapper.find('[data-test="sidebar-credits"]').text()).toContain('240')
    wrapper.unmount()
  })

  it('toasts when the user cannot be loaded', async () => {
    fetchUser.mockRejectedValue(new Error('Failed to load user (500)'))
    const { wrapper } = await mountApp('/')
    expect(useToasts().toasts.value[0]).toMatchObject({ type: 'error', message: 'Failed to load user (500)' })
    wrapper.unmount()
  })

  it('logs out to the login page, even if the request fails', async () => {
    for (const outcome of ['ok', 'fail']) {
      if (outcome === 'fail') logout.mockRejectedValueOnce(new Error('offline'))
      const { wrapper, router } = await mountApp('/')
      await wrapper.find('[data-test="user-menu-toggle"]').trigger('click')
      await wrapper.find('[data-test="logout"]').trigger('click')
      await flushPromises()
      expect(router.currentRoute.value.path).toBe('/login')
      expect(wrapper.find('[data-test="sidebar"]').exists()).toBe(false)
      wrapper.unmount()
    }
  })
})
