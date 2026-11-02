// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

vi.mock('../src/api', () => ({
  listSetupUsers: vi.fn(),
  adjustUserCredits: vi.fn(),
  listUserCreditAdjustments: vi.fn(),
}))

import { listSetupUsers, listUserCreditAdjustments } from '../src/api'
import SetupUsers from '../src/components/SetupUsers.vue'
import { useToasts } from '../src/composables/useToasts'

function adminUser(overrides = {}) {
  return {
    id: 'u1',
    email: 'ada@example.com',
    name: 'Ada Lovelace',
    picture: '',
    provider: 'github',
    credits: 1500,
    creditsLimit: 2000,
    creditsRenewAt: '2026-11-20T00:00:00Z',
    createdAt: '2026-10-21T00:00:00Z',
    paused: false,
    usage: { folders: 1, storageBytes: 2048, storageBlocks: 1, domains: 0, monthlyCredits: 1000 },
    ...overrides,
  }
}

function err(message, status) {
  return Object.assign(new Error(message), { status })
}

async function mountList() {
  const wrapper = mount(SetupUsers)
  await flushPromises()
  return wrapper
}

describe('SetupUsers', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useToasts().toasts.value = []
    listUserCreditAdjustments.mockResolvedValue({ adjustments: [], next: '' })
  })
  afterEach(() => {
    useToasts().toasts.value = []
  })

  it('shows loading, then the users with balance and usage', async () => {
    let resolve
    listSetupUsers.mockReturnValueOnce(new Promise((r) => (resolve = r)))
    const wrapper = mount(SetupUsers)
    expect(wrapper.find('[data-test="users-loading"]').exists()).toBe(true)
    resolve({
      users: [adminUser(), adminUser({ id: 'u2', name: '', email: 'grace@example.com', credits: -250, paused: true })],
      next: '',
    })
    await flushPromises()
    expect(listSetupUsers).toHaveBeenCalledWith({ q: '' })
    const ada = wrapper.find('[data-test="users-row-u1"]')
    expect(ada.text()).toContain('Ada Lovelace')
    expect(ada.text()).toContain('ada@example.com · 2,000 / month')
    expect(ada.text()).toContain('2.0 KB')
    expect(wrapper.find('[data-test="users-paused-u1"]').exists()).toBe(false)
    const grace = wrapper.find('[data-test="users-row-u2"]')
    expect(grace.text()).toContain('grace@example.com')
    expect(wrapper.find('[data-test="users-balance-u2"]').classes()).toContain('text-rose-600')
    expect(wrapper.find('[data-test="users-paused-u2"]').text()).toBe('Paused')
    expect(wrapper.find('[data-test="users-more"]').exists()).toBe(false)
  })

  it('says when there are no users', async () => {
    listSetupUsers.mockResolvedValueOnce({ users: [], next: '' })
    const wrapper = await mountList()
    expect(wrapper.find('[data-test="users-empty"]').text()).toBe('No users yet.')
  })

  it('searches, clears and loads more', async () => {
    listSetupUsers.mockResolvedValueOnce({ users: [adminUser()], next: 'u1' })
    const wrapper = await mountList()

    listSetupUsers.mockResolvedValueOnce({ users: [adminUser({ id: 'u0', name: 'Older' })], next: '' })
    await wrapper.find('[data-test="users-more"]').trigger('click')
    await flushPromises()
    expect(listSetupUsers).toHaveBeenLastCalledWith({ q: '', before: 'u1' })
    expect(wrapper.findAll('li')).toHaveLength(2)
    expect(wrapper.find('[data-test="users-more"]').exists()).toBe(false)

    listSetupUsers.mockResolvedValueOnce({ users: [], next: '' })
    await wrapper.find('[data-test="users-search-input"]').setValue('  nobody ')
    await wrapper.find('[data-test="users-search"]').trigger('submit')
    await flushPromises()
    expect(listSetupUsers).toHaveBeenLastCalledWith({ q: 'nobody' })
    expect(wrapper.find('[data-test="users-empty"]').text()).toBe('No user matches “nobody”.')

    listSetupUsers.mockResolvedValueOnce({ users: [adminUser()], next: '' })
    await wrapper.find('[data-test="users-search-clear"]').trigger('click')
    await flushPromises()
    expect(listSetupUsers).toHaveBeenLastCalledWith({ q: '' })
    expect(wrapper.find('[data-test="users-search-input"]').element.value).toBe('')
    expect(wrapper.find('[data-test="users-search-clear"]').exists()).toBe(false)
  })

  it('toasts failures and reports an expired session', async () => {
    listSetupUsers.mockRejectedValueOnce(err('Failed to load users (500)', 500))
    const wrapper = await mountList()
    expect(useToasts().toasts.value.at(-1)).toMatchObject({ type: 'error', message: 'Failed to load users (500)' })
    expect(wrapper.emitted('expired')).toBeUndefined()

    listSetupUsers.mockResolvedValueOnce({ users: [adminUser()], next: 'u1' })
    await wrapper.find('[data-test="users-search"]').trigger('submit')
    await flushPromises()
    listSetupUsers.mockRejectedValueOnce(err('unauthorized', 401))
    await wrapper.find('[data-test="users-more"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('expired')).toHaveLength(1)
    expect(wrapper.findAll('li')).toHaveLength(1)
  })

  it('opens a user, keeps the list in step with an adjustment and goes back', async () => {
    listSetupUsers.mockResolvedValueOnce({ users: [adminUser(), adminUser({ id: 'u2', name: 'Grace' })], next: '' })
    const wrapper = await mountList()
    await wrapper.find('[data-test="users-row-u1"]').trigger('click')
    await flushPromises()
    const panel = wrapper.findComponent({ name: 'SetupUserCredits' })
    expect(panel.props('user').id).toBe('u1')
    expect(wrapper.find('[data-test="setup-users"]').exists()).toBe(false)

    panel.vm.$emit('updated', adminUser({ credits: 2000 }))
    await flushPromises()
    expect(wrapper.find('[data-test="user-credits-balance"]').text()).toBe('2,000')

    panel.vm.$emit('expired')
    expect(wrapper.emitted('expired')).toHaveLength(1)

    await wrapper.find('[data-test="user-credits-back"]').trigger('click')
    expect(wrapper.find('[data-test="users-balance-u1"]').text()).toBe('2,000')
    expect(wrapper.find('[data-test="users-balance-u2"]').text()).toBe('1,500')
  })
})
