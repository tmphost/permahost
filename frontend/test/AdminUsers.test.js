// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

vi.mock('../src/api', () => ({ listAdminUsers: vi.fn() }))

import AdminUsers from '../src/components/AdminUsers.vue'
import { listAdminUsers } from '../src/api'

const user = (id, extra = {}) => ({
  id,
  email: `${id}@x.io`,
  name: '',
  credits: 1500,
  creditsLimit: 2000,
  paused: false,
  used: 3,
  usage: { folders: 1, storageBytes: 2048, storageBlocks: 1, domains: 2, monthlyCredits: 1500 },
  createdAt: '2026-10-01T00:00:00Z',
  ...extra,
})

function deferred() {
  let resolve
  const promise = new Promise((r) => {
    resolve = r
  })
  return { promise, resolve }
}

describe('AdminUsers', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('lists users, loads more, and opens one', async () => {
    listAdminUsers.mockResolvedValueOnce({ users: [user('b', { name: 'Bea' })], next: 'b' })
    const wrapper = mount(AdminUsers)
    expect(wrapper.find('[data-test="admin-users-loading"]').exists()).toBe(true)
    await flushPromises()
    expect(listAdminUsers).toHaveBeenCalledWith({ q: '' })

    const row = wrapper.find('[data-test="admin-user-b"]')
    expect(row.text()).toContain('Bea')
    expect(row.text()).toContain('b@x.io')
    expect(row.text()).toContain((1500).toLocaleString())
    expect(row.text()).toContain(`${(2000).toLocaleString()} / month`)
    expect(row.text()).toContain('2.0 KB')
    expect(wrapper.find('[data-test="admin-user-paused-b"]').exists()).toBe(false)

    const more = deferred()
    listAdminUsers.mockReturnValueOnce(more.promise)
    await wrapper.find('[data-test="admin-users-more"]').trigger('click')
    expect(listAdminUsers).toHaveBeenLastCalledWith({ q: '', before: 'b' })
    expect(wrapper.find('[data-test="admin-users-more"]').attributes('disabled')).toBeDefined()
    more.resolve({ users: [user('a')], next: '' })
    await flushPromises()
    expect(wrapper.find('[data-test="admin-user-a"]').text()).toContain('a@x.io')
    expect(wrapper.find('[data-test="admin-users-more"]').exists()).toBe(false)

    await wrapper.find('[data-test="admin-user-a"]').trigger('click')
    expect(wrapper.emitted('open')).toEqual([['a']])
  })

  it('marks paused accounts', async () => {
    listAdminUsers.mockResolvedValueOnce({ users: [user('p', { credits: -500, paused: true })], next: '' })
    const wrapper = mount(AdminUsers)
    await flushPromises()
    expect(wrapper.find('[data-test="admin-user-balance-p"]').classes()).toContain('text-rose-600')
    expect(wrapper.find('[data-test="admin-user-paused-p"]').text()).toBe('Paused')
  })

  it('searches, says when nothing matches, and clears', async () => {
    listAdminUsers.mockResolvedValueOnce({ users: [], next: '' })
    const wrapper = mount(AdminUsers)
    await flushPromises()
    expect(wrapper.find('[data-test="admin-users-empty"]').text()).toBe('No users yet.')
    expect(wrapper.find('[data-test="admin-search-clear"]').exists()).toBe(false)

    listAdminUsers.mockResolvedValueOnce({ users: [], next: '' })
    await wrapper.find('[data-test="admin-search-input"]').setValue('  zed ')
    await wrapper.find('[data-test="admin-search"]').trigger('submit')
    await flushPromises()
    expect(listAdminUsers).toHaveBeenLastCalledWith({ q: 'zed' })
    expect(wrapper.find('[data-test="admin-users-empty"]').text()).toBe('No user matches “zed”.')

    listAdminUsers.mockResolvedValueOnce({ users: [user('a')], next: '' })
    await wrapper.find('[data-test="admin-search-clear"]').trigger('click')
    await flushPromises()
    expect(listAdminUsers).toHaveBeenLastCalledWith({ q: '' })
    expect(wrapper.find('[data-test="admin-search-input"]').element.value).toBe('')
    expect(wrapper.find('[data-test="admin-user-a"]').exists()).toBe(true)
  })

  it('reports failed loads', async () => {
    const err = new Error('Failed to load users (500)')
    listAdminUsers.mockRejectedValueOnce(err)
    const wrapper = mount(AdminUsers)
    await flushPromises()
    expect(wrapper.emitted('error')).toEqual([[err]])

    listAdminUsers.mockResolvedValueOnce({ users: [user('b')], next: 'b' })
    await wrapper.find('[data-test="admin-search"]').trigger('submit')
    await flushPromises()
    listAdminUsers.mockRejectedValueOnce(err)
    await wrapper.find('[data-test="admin-users-more"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('error')).toHaveLength(2)
    expect(wrapper.find('[data-test="admin-users-more"]').attributes('disabled')).toBeUndefined()
  })
})
