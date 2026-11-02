// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

vi.mock('../src/api', () => ({ adjustUserCredits: vi.fn(), fetchAdminUser: vi.fn(), listAdminUserCreditActivities: vi.fn() }))

import AdminUserDetail from '../src/components/AdminUserDetail.vue'
import { adjustUserCredits, fetchAdminUser, listAdminUserCreditActivities } from '../src/api'
import { useToasts } from '../src/composables/useToasts'

const DAY_MS = 24 * 60 * 60 * 1000
const ago = (days) => new Date(Date.now() - days * DAY_MS).toISOString()

const USER = {
  id: 'u1',
  email: 'ann@example.com',
  name: 'Ann',
  picture: 'https://example.com/a.png',
  provider: 'github',
  credits: 300,
  creditsLimit: 2000,
  creditsRenewAt: ago(-3),
  createdAt: '2026-10-01T00:00:00Z',
  paused: false,
  used: 1500,
  usage: { folders: 2, storageBytes: 3 << 20, storageBlocks: 1, domains: 1, monthlyCredits: 2000 },
}

const mountDetail = () => mount(AdminUserDetail, { props: { id: 'u1' } })

function err(message, status) {
  return Object.assign(new Error(message), { status })
}

// ready mounts the detail of USER (with overrides) and no history.
async function ready(overrides = {}) {
  fetchAdminUser.mockResolvedValue({ ...USER, ...overrides })
  listAdminUserCreditActivities.mockResolvedValue({ activities: [], next: '' })
  const wrapper = mountDetail()
  await flushPromises()
  return wrapper
}

describe('AdminUserDetail', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useToasts().toasts.value = []
  })
  afterEach(() => {
    useToasts().toasts.value = []
  })

  it('shows the user, their usage and their credit history', async () => {
    fetchAdminUser.mockResolvedValue(USER)
    listAdminUserCreditActivities
      .mockResolvedValueOnce({ activities: [{ id: 'c', ops: 'spend', change: -500, current: 300, createdAt: ago(0) }], next: 'c' })
      .mockResolvedValueOnce({
        activities: [{ id: 'b', ops: 'admin_grant', change: 800, current: 800, actor: 'root', note: 'outage refund', createdAt: ago(40) }],
        next: 'b',
      })
      .mockResolvedValueOnce({ activities: [{ id: 'a', ops: 'grant', change: 2000, current: 2000, createdAt: ago(50) }], next: '' })
    const wrapper = mountDetail()
    expect(wrapper.find('[data-test="admin-user-loading"]').exists()).toBe(true)
    await flushPromises()

    expect(fetchAdminUser).toHaveBeenCalledWith('u1')
    expect(listAdminUserCreditActivities.mock.calls).toEqual([['u1', ''], ['u1', 'c']])
    expect(wrapper.find('[data-test="admin-user-name"]').text()).toBe('Ann')
    expect(wrapper.find('[data-test="admin-user-id"]').text()).toBe('u1')
    expect(wrapper.text()).toContain('github')
    expect(wrapper.find('img').attributes('src')).toBe(USER.picture)
    expect(wrapper.find('[data-test="admin-user-paused"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="admin-user-balance"]').text()).toBe('300')
    expect(wrapper.find('[data-test="admin-user-used"]').text()).toBe((1500).toLocaleString())
    expect(wrapper.find('[data-test="admin-user-monthly"]').text()).toBe((2000).toLocaleString())
    expect(wrapper.find('[data-test="admin-user-renewal"]').text()).toBe('Renews in 3 days')
    expect(wrapper.find('[data-test="admin-user-cost"]').text()).toBe((2000).toLocaleString())
    expect(wrapper.find('[data-test="admin-user-folders"]').text()).toBe('2')
    expect(wrapper.find('[data-test="admin-user-storage"]').text()).toBe('3.0 MB')
    expect(wrapper.find('[data-test="admin-user-domains"]').text()).toBe('1')
    expect(wrapper.find('[data-test="credits-chart-used"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="credits-chart-balance"]').exists()).toBe(true)
    const rows = wrapper.findAll('[data-test="credits-row"]')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('Hosting charge')
    expect(rows[1].find('[data-test="credits-actor"]').text()).toContain('root')
    expect(rows[1].find('[data-test="credits-note"]').text()).toBe('outage refund')
    expect(rows[1].find('[data-test="credits-change"]').classes()).toContain('text-emerald-600')

    await wrapper.find('[data-test="credits-older"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-test="credits-row"]')).toHaveLength(3)
    expect(wrapper.text()).toContain('Sign-up grant')

    await wrapper.find('[data-test="admin-user-back"]').trigger('click')
    expect(wrapper.emitted('back')).toHaveLength(1)
  })

  it('falls back to the email without a name or picture, and says what resumes a paused account', async () => {
    const wrapper = await ready({ name: '', picture: '', provider: '', credits: -250, paused: true })
    expect(wrapper.find('[data-test="admin-user-name"]').text()).toBe('ann@example.com')
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.find('[data-test="credits-empty"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="admin-user-paused"]').text()).toContain('Grant at least 250 credits')
  })

  it('says when no user has the ID', async () => {
    fetchAdminUser.mockRejectedValue(err('user not found', 404))
    const wrapper = mountDetail()
    await flushPromises()
    expect(wrapper.find('[data-test="admin-user-missing"]').text()).toContain('u1')
    expect(listAdminUserCreditActivities).not.toHaveBeenCalled()
    expect(wrapper.emitted('error')).toBeUndefined()
  })

  it('reports other failures', async () => {
    const e = new Error('Failed to load user (500)')
    fetchAdminUser.mockRejectedValue(e)
    const wrapper = mountDetail()
    await flushPromises()
    expect(wrapper.find('[data-test="admin-user-failed"]').exists()).toBe(true)
    expect(wrapper.emitted('error')).toEqual([[e]])
  })

  it('reports a failed history load', async () => {
    const e = new Error('Failed to load credit history (500)')
    fetchAdminUser.mockResolvedValue(USER)
    listAdminUserCreditActivities.mockRejectedValue(e)
    const wrapper = mountDetail()
    await flushPromises()
    expect(wrapper.emitted('error')).toEqual([[e]])
    expect(wrapper.find('[data-test="admin-user-name"]').exists()).toBe(true)
  })

  it('reports a failed load of older history', async () => {
    const e = new Error('Failed to load credit history (500)')
    fetchAdminUser.mockResolvedValue(USER)
    listAdminUserCreditActivities.mockResolvedValueOnce({ activities: [{ id: 'a', ops: 'grant', change: 2000, current: 2000, createdAt: ago(40) }], next: 'a' })
    const wrapper = mountDetail()
    await flushPromises()
    listAdminUserCreditActivities.mockRejectedValueOnce(e)
    await wrapper.find('[data-test="credits-older"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('error')).toEqual([[e]])
    expect(wrapper.find('[data-test="credits-older"]').attributes('disabled')).toBeUndefined()
  })

  it('needs a whole positive amount and a note, and never deducts below zero', async () => {
    const wrapper = await ready()
    const save = () => wrapper.find('[data-test="admin-adjust-save"]')
    const preview = () => wrapper.find('[data-test="admin-adjust-preview"]').text()
    expect(save().attributes('disabled')).toBeDefined()
    expect(preview()).toBe('A whole number above zero.')

    await wrapper.find('[data-test="admin-adjust-amount"]').setValue('2.5')
    expect(preview()).toBe('A whole number above zero.')
    await wrapper.find('[data-test="admin-adjust-amount"]').setValue('500')
    expect(preview()).toBe('Balance after: 800')
    await wrapper.find('[data-test="admin-adjust-note"]').setValue('   ')
    expect(save().attributes('disabled')).toBeDefined()
    await wrapper.find('[data-test="admin-adjust-note"]').setValue('refund')
    expect(save().attributes('disabled')).toBeUndefined()
    expect(save().text()).toBe('Grant credits')

    await wrapper.find('[data-test="admin-mode-deduct"]').trigger('click')
    expect(wrapper.find('[data-test="admin-mode-deduct"]').attributes('aria-checked')).toBe('true')
    expect(save().text()).toBe('Deduct credits')
    expect(preview()).toBe('At most 300 can be deducted.')
    expect(save().attributes('disabled')).toBeDefined()
    await wrapper.find('[data-test="admin-adjust-amount"]').setValue('300')
    expect(preview()).toBe('Balance after: 0')
    expect(save().attributes('disabled')).toBeUndefined()
  })

  it('cannot deduct from a balance below zero', async () => {
    const wrapper = await ready({ credits: -10, paused: true })
    await wrapper.find('[data-test="admin-mode-deduct"]').trigger('click')
    await wrapper.find('[data-test="admin-adjust-amount"]').setValue('1')
    expect(wrapper.find('[data-test="admin-adjust-preview"]').text()).toBe('At most 0 can be deducted.')
  })

  it('grants, disabling the button while saving, then toasts and adds the history entry', async () => {
    const wrapper = await ready({ credits: -500, paused: true })
    await wrapper.find('[data-test="admin-adjust-amount"]').setValue('1500')
    await wrapper.find('[data-test="admin-adjust-note"]').setValue('  goodwill ')
    let resolve
    adjustUserCredits.mockReturnValueOnce(new Promise((r) => (resolve = r)))
    await wrapper.find('[data-test="admin-adjust"]').trigger('submit')
    expect(adjustUserCredits).toHaveBeenCalledWith('u1', 1500, 'goodwill')
    expect(wrapper.find('[data-test="admin-adjust-save"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-test="admin-adjust-save"]').text()).toBe('Saving...')

    resolve({
      user: { ...USER, credits: 1000, paused: false },
      activity: { id: 'n', ops: 'admin_grant', change: 1500, current: 1000, actor: 'root', note: 'goodwill', createdAt: ago(0) },
    })
    await flushPromises()
    expect(useToasts().toasts.value.at(-1)).toMatchObject({ type: 'success', message: `Granted ${(1500).toLocaleString()} credits. Balance: ${(1000).toLocaleString()}.` })
    expect(wrapper.find('[data-test="admin-user-balance"]').text()).toBe((1000).toLocaleString())
    expect(wrapper.find('[data-test="admin-user-paused"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="credits-note"]').text()).toBe('goodwill')
    expect(wrapper.find('[data-test="admin-adjust-amount"]').element.value).toBe('')
    expect(wrapper.find('[data-test="admin-adjust-note"]').element.value).toBe('')
  })

  it('deducts with a negative amount', async () => {
    const wrapper = await ready()
    await wrapper.find('[data-test="admin-mode-deduct"]').trigger('click')
    await wrapper.find('[data-test="admin-adjust-amount"]').setValue('100')
    await wrapper.find('[data-test="admin-adjust-note"]').setValue('chargeback')
    adjustUserCredits.mockResolvedValueOnce({
      user: { ...USER, credits: 200 },
      activity: { id: 'n', ops: 'admin_deduct', change: -100, current: 200, actor: 'root', note: 'chargeback', createdAt: ago(0) },
    })
    await wrapper.find('[data-test="admin-adjust"]').trigger('submit')
    await flushPromises()
    expect(adjustUserCredits).toHaveBeenCalledWith('u1', -100, 'chargeback')
    expect(useToasts().toasts.value.at(-1)).toMatchObject({ type: 'success', message: 'Deducted 100 credits. Balance: 200.' })
    expect(wrapper.text()).toContain('Root deduction')
  })

  it('keeps the form and reports a refused adjustment', async () => {
    const wrapper = await ready()
    await wrapper.find('[data-test="admin-adjust-amount"]').setValue('5')
    await wrapper.find('[data-test="admin-adjust-note"]').setValue('x')
    const e = err('the deduction would take the balance below zero', 409)
    adjustUserCredits.mockRejectedValueOnce(e)
    await wrapper.find('[data-test="admin-adjust"]').trigger('submit')
    await flushPromises()
    expect(wrapper.emitted('error')).toEqual([[e]])
    expect(wrapper.find('[data-test="admin-adjust-note"]').element.value).toBe('x')
    expect(wrapper.find('[data-test="admin-adjust-save"]').attributes('disabled')).toBeUndefined()
  })
})
