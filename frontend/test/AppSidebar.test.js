// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createPinia } from 'pinia'
import AppSidebar from '../src/components/AppSidebar.vue'
import { clickOutside } from '../src/app'

const user = {
  id: 'u1',
  name: 'Ada Lovelace',
  email: 'ada@example.com',
  picture: '',
  credits: 125,
  creditsLimit: 250,
  creditsRenewAt: new Date(Date.now() + 10 * 86400000 - 1000).toISOString(),
}

async function mountSidebar(props = {}) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: { render: () => null } }, { path: '/other', component: { render: () => null } }, { path: '/files', component: { render: () => null } }, { path: '/connect', component: { render: () => null } }],
  })
  await router.push('/')
  return mount(AppSidebar, {
    props: { user, ...props },
    attachTo: document.body,
    global: { plugins: [router, createPinia()], directives: { 'click-outside': clickOutside } },
  })
}

describe('AppSidebar', () => {
  it('shows brand, nav, credits and the user', async () => {
    const wrapper = await mountSidebar()
    expect(wrapper.find('[data-test="sidebar-brand"]').text()).toBe('tmphost.io')
    expect(wrapper.text()).not.toContain('Home')
    expect(wrapper.findAll('a').find((a) => a.text() === 'Files').attributes('href')).toBe('/')
    expect(wrapper.find('a[href="/connect"]').text()).toBe('Connect')
    expect(wrapper.find('a[href="/connect"]').classes()).not.toContain('font-semibold')
    const credits = wrapper.find('[data-test="sidebar-credits"]')
    expect(credits.text()).toContain('125')
    expect(credits.text()).toContain('/ 250')
    expect(credits.text()).toContain('Renews in 10 days')
    expect(wrapper.find('[data-test="credits-bar"]').attributes('style')).toContain('width: 50%')
    expect(wrapper.text()).toContain('Ada Lovelace')
    expect(wrapper.find('[data-test="user-menu-toggle"]').text()).toContain('A')
    expect(wrapper.find('a[href="/"].font-semibold').exists()).toBe(true)
    wrapper.unmount()
  })

  it('hides labels when collapsed and shows them in the mobile drawer', async () => {
    const collapsed = await mountSidebar({ collapsed: true })
    expect(collapsed.find('[data-test="sidebar-brand"]').exists()).toBe(false)
    expect(collapsed.find('[data-test="sidebar-credits"]').exists()).toBe(false)
    expect(collapsed.find('[data-test="sidebar-toggle"]').attributes('title')).toBe('Expand sidebar')
    expect(collapsed.find('a[title="Files"]').exists()).toBe(true)
    collapsed.unmount()

    const drawer = await mountSidebar({ collapsed: true, mobileOpen: true })
    expect(drawer.find('[data-test="sidebar-brand"]').exists()).toBe(true)
    expect(drawer.find('[data-test="sidebar"]').classes()).toContain('w-[280px]')
    drawer.unmount()
  })

  it('emits toggle-collapse', async () => {
    const wrapper = await mountSidebar()
    expect(wrapper.find('[data-test="sidebar-toggle"]').attributes('title')).toBe('Collapse sidebar')
    await wrapper.find('[data-test="sidebar-toggle"]').trigger('click')
    expect(wrapper.emitted('toggle-collapse')).toHaveLength(1)
    wrapper.unmount()
  })

  it('opens the user menu, closes it on outside click, and logs out', async () => {
    const wrapper = await mountSidebar()
    await wrapper.find('[data-test="user-menu-toggle"]').trigger('click')
    const menu = wrapper.find('[data-test="user-menu"]')
    expect(menu.text()).toContain('ada@example.com')

    document.body.click()
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-test="user-menu"]').exists()).toBe(false)

    await wrapper.find('[data-test="user-menu-toggle"]').trigger('click')
    await wrapper.find('[data-test="logout"]').trigger('click')
    expect(wrapper.emitted('logout')).toHaveLength(1)
    expect(wrapper.find('[data-test="user-menu"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('switches the theme from the user menu', async () => {
    localStorage.clear()
    const wrapper = await mountSidebar()
    await wrapper.find('[data-test="user-menu-toggle"]').trigger('click')
    expect(wrapper.find('[data-test="theme-system"]').attributes('aria-checked')).toBe('true')

    await wrapper.find('[data-test="theme-dark"]').trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(localStorage.getItem('theme')).toBe('dark')
    expect(wrapper.find('[data-test="theme-dark"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.find('[data-test="theme-system"]').attributes('aria-checked')).toBe('false')
    // Picking a theme keeps the menu open.
    expect(wrapper.find('[data-test="user-menu"]').exists()).toBe(true)

    await wrapper.find('[data-test="theme-light"]').trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    localStorage.clear()
    wrapper.unmount()
  })

  it('positions the menu beside a collapsed sidebar', async () => {
    const wrapper = await mountSidebar({ collapsed: true })
    await wrapper.find('[data-test="user-menu-toggle"]').trigger('click')
    expect(wrapper.find('[data-test="user-menu"]').classes()).toContain('left-full')
    wrapper.unmount()
  })

  it('handles a picture, an email-only user and no user', async () => {
    const withPicture = await mountSidebar({ user: { ...user, picture: 'https://img/p.png' } })
    expect(withPicture.find('img').attributes('src')).toBe('https://img/p.png')
    withPicture.unmount()

    const emailOnly = await mountSidebar({ user: { ...user, name: '', creditsRenewAt: 'bad' } })
    expect(emailOnly.find('[data-test="user-menu-toggle"]').text()).toContain('ada@example.com')
    expect(emailOnly.find('[data-test="sidebar-credits"]').text()).not.toContain('Renews')
    await emailOnly.find('[data-test="user-menu-toggle"]').trigger('click')
    // The email is the display name, so it is not repeated under it.
    expect(emailOnly.find('[data-test="user-menu"]').text().split('ada@example.com')).toHaveLength(2)
    emailOnly.unmount()

    const none = await mountSidebar({ user: null })
    expect(none.find('[data-test="sidebar-credits"]').exists()).toBe(false)
    expect(none.find('[data-test="user-menu-toggle"]').text()).toContain('Account')
    none.unmount()
  })
})
