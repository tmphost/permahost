// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'

vi.mock('../src/api', () => ({
  fetchAuthConfig: vi.fn(),
  providerLoginUrl: (provider, redirect) => `/auth/${provider}/login${redirect ? `?redirect_url=${redirect}` : ''}`,
}))

import { fetchAuthConfig } from '../src/api'
import LoginView from '../src/views/LoginView.vue'
import { useToasts } from '../src/composables/useToasts'

const GOOGLE = { id: 'google', name: 'Google', icon: '' }
const GITHUB = { id: 'github', name: 'GitHub', icon: '' }

async function mountAt(path) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/login', component: LoginView }, { path: '/setup', component: { render: () => null } }, { path: '/upload', component: { render: () => null } }] })
  await router.push(path)
  const wrapper = mount(LoginView, { global: { plugins: [router] } })
  return wrapper
}

describe('LoginView', () => {
  beforeEach(() => {
    useToasts().toasts.value = []
  })
  afterEach(() => {
    useToasts().toasts.value = []
  })

  it('shows the brand and a spinner while loading', async () => {
    fetchAuthConfig.mockReturnValue(new Promise(() => {}))
    const wrapper = await mountAt('/login')
    expect(wrapper.text()).toContain('tmphost.io')
    expect(wrapper.find('[data-test="brand-logo"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="login-loading"]').exists()).toBe(true)
  })

  it('renders a button per provider, carrying the redirect', async () => {
    const okta = { id: 'okta', name: 'Okta', icon: '<svg xmlns="http://www.w3.org/2000/svg"><script>x</script></svg>' }
    const bare = { id: 'bare', name: 'Bare', icon: '' }
    fetchAuthConfig.mockResolvedValue([GOOGLE, GITHUB, okta, bare])
    const wrapper = await mountAt('/login?redirect=/deep')
    await flushPromises()
    const google = wrapper.find('[data-test="login-google"]')
    const github = wrapper.find('[data-test="login-github"]')
    expect(google.text()).toContain('Continue with Google')
    expect(google.attributes('href')).toBe('/auth/google/login?redirect_url=/deep')
    expect(github.attributes('href')).toBe('/auth/github/login?redirect_url=/deep')
    expect(github.find('svg').exists()).toBe(true)
    const custom = wrapper.find('[data-test="login-okta"]')
    expect(custom.text()).toContain('Continue with Okta')
    expect(custom.attributes('href')).toBe('/auth/okta/login?redirect_url=/deep')
    // The SVG is an <img> source, never markup in the page.
    expect(custom.find('script').exists()).toBe(false)
    expect(custom.find('[data-test="login-icon"]').attributes('src')).toBe(
      `data:image/svg+xml;charset=utf-8,${encodeURIComponent(okta.icon)}`,
    )
    const plain = wrapper.find('[data-test="login-bare"]')
    expect(plain.find('img').exists()).toBe(false)
    expect(plain.find('svg').exists()).toBe(false)
    expect(wrapper.text()).toContain('250 free operations')
    expect(wrapper.find('[data-test="login-setup"]').attributes('href')).toBe('/setup')
    expect(wrapper.find('[data-test="login-upload"]').attributes('href')).toBe('/upload')
  })

  it('ignores a non-string redirect', async () => {
    fetchAuthConfig.mockResolvedValue([GOOGLE])
    const wrapper = await mountAt('/login?redirect=a&redirect=b')
    await flushPromises()
    expect(wrapper.find('[data-test="login-google"]').attributes('href')).toBe('/auth/google/login')
  })

  it('explains when no provider is configured', async () => {
    fetchAuthConfig.mockResolvedValue([])
    const wrapper = await mountAt('/login')
    await flushPromises()
    expect(wrapper.find('[data-test="login-no-providers"]').exists()).toBe(true)
  })

  it('reports a config failure', async () => {
    fetchAuthConfig.mockRejectedValue(new Error('Failed to load sign-in options (503)'))
    const wrapper = await mountAt('/login')
    await flushPromises()
    expect(wrapper.find('[data-test="login-config-error"]').text()).toBe('Sign-in is unavailable right now.')
    expect(useToasts().toasts.value[0]).toMatchObject({ type: 'error', message: 'Failed to load sign-in options (503)' })
  })

  it('shows the callback error from the query', async () => {
    fetchAuthConfig.mockResolvedValue([GOOGLE])
    const wrapper = await mountAt('/login?error=invalid_state')
    await flushPromises()
    expect(wrapper.find('[data-test="login-error"]').text()).toContain('sign-in session expired')
  })
})
