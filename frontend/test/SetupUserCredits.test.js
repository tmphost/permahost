// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

vi.mock('../src/api', () => ({
  adjustUserCredits: vi.fn(),
  listUserCreditAdjustments: vi.fn(),
}))

import { adjustUserCredits, listUserCreditAdjustments } from '../src/api'
import SetupUserCredits from '../src/components/SetupUserCredits.vue'
import { useToasts } from '../src/composables/useToasts'

function user(overrides = {}) {
  return {
    id: 'u1',
    email: 'ada@example.com',
    name: 'Ada Lovelace',
    picture: 'https://example.com/ada.png',
    provider: 'github',
    credits: 300,
    creditsLimit: 2000,
    creditsRenewAt: '2999-01-01T00:00:00Z',
    createdAt: '2026-10-21T00:00:00Z',
    paused: false,
    usage: { folders: 2, storageBytes: 3 << 20, storageBlocks: 1, domains: 1, monthlyCredits: 2000 },
    ...overrides,
  }
}

function adjustment(overrides = {}) {
  return { id: 'a1', userId: 'u1', actor: 'root', amount: 500, note: 'refund', balanceAfter: 800, createdAt: '2026-11-02T07:00:00Z', ...overrides }
}

function err(message, status) {
  return Object.assign(new Error(message), { status })
}

async function mountPanel(props = {}) {
  const wrapper = mount(SetupUserCredits, { props: { user: user(), ...props } })
  await flushPromises()
  return wrapper
}

describe('SetupUserCredits', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useToasts().toasts.value = []
    listUserCreditAdjustments.mockResolvedValue({ adjustments: [], next: '' })
  })
  afterEach(() => {
    useToasts().toasts.value = []
  })

  it('shows the user, the balance and the usage', async () => {
    let resolve
    listUserCreditAdjustments.mockReturnValueOnce(new Promise((r) => (resolve = r)))
    const wrapper = mount(SetupUserCredits, { props: { user: user() } })
    expect(wrapper.find('[data-test="user-credits-history-loading"]').exists()).toBe(true)
    resolve({ adjustments: [], next: '' })
    await flushPromises()

    expect(listUserCreditAdjustments).toHaveBeenCalledWith('u1', '')
    expect(wrapper.find('[data-test="user-credits-name"]').text()).toBe('Ada Lovelace')
    expect(wrapper.find('img').attributes('src')).toBe('https://example.com/ada.png')
    expect(wrapper.text()).toContain('u1 · github')
    expect(wrapper.find('[data-test="user-credits-balance"]').text()).toBe('300')
    expect(wrapper.find('[data-test="user-credits-paused"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="user-credits-grant"]').text()).toContain('2,000')
    expect(wrapper.find('[data-test="user-credits-grant"]').text()).toMatch(/Renews in \d+ days/)
    expect(wrapper.find('[data-test="user-credits-cost"]').text()).toContain('2,000')
    expect(wrapper.find('[data-test="user-credits-folders"]').text()).toContain('2')
    expect(wrapper.find('[data-test="user-credits-storage"]').text()).toContain('3.0 MB')
    expect(wrapper.find('[data-test="user-credits-domains"]').text()).toContain('1')
    expect(wrapper.find('[data-test="user-credits-history-empty"]').exists()).toBe(true)

    await wrapper.find('[data-test="user-credits-back"]').trigger('click')
    expect(wrapper.emitted('back')).toHaveLength(1)
  })

  it('names a user without a name or picture by email, and says what resumes a paused one', async () => {
    const wrapper = await mountPanel({ user: user({ name: '', picture: '', provider: '', credits: -250, paused: true }) })
    expect(wrapper.find('[data-test="user-credits-name"]').text()).toBe('ada@example.com')
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('u1 ·')
    expect(wrapper.find('[data-test="user-credits-balance"]').classes()).toContain('text-rose-600')
    expect(wrapper.find('[data-test="user-credits-paused"]').text()).toContain('Grant at least 250 credits')
  })

  it('lists the history and loads older entries', async () => {
    listUserCreditAdjustments.mockResolvedValueOnce({
      adjustments: [adjustment(), adjustment({ id: 'a0', amount: -1200, note: 'abuse', balanceAfter: 300 })],
      next: 'a0',
    })
    const wrapper = await mountPanel()
    const grant = wrapper.find('[data-test="user-credits-entry-a1"]')
    expect(grant.text()).toContain('+500')
    expect(grant.text()).toContain('refund')
    expect(grant.text()).toContain('root ·')
    expect(grant.text()).toContain('800')
    expect(grant.find('span').classes()).toContain('text-emerald-600')
    const deduction = wrapper.find('[data-test="user-credits-entry-a0"]')
    expect(deduction.text()).toContain('−1,200')
    expect(deduction.find('span').classes()).toContain('text-rose-600')

    listUserCreditAdjustments.mockResolvedValueOnce({ adjustments: [adjustment({ id: 'z', note: 'first' })], next: '' })
    await wrapper.find('[data-test="user-credits-older"]').trigger('click')
    await flushPromises()
    expect(listUserCreditAdjustments).toHaveBeenLastCalledWith('u1', 'a0')
    expect(wrapper.findAll('li')).toHaveLength(3)
    expect(wrapper.find('[data-test="user-credits-older"]').exists()).toBe(false)
  })

  it('toasts a history failure and reports an expired session', async () => {
    listUserCreditAdjustments.mockRejectedValueOnce(err('Failed to load credit history (500)', 500))
    let wrapper = await mountPanel()
    expect(useToasts().toasts.value.at(-1)).toMatchObject({ type: 'error', message: 'Failed to load credit history (500)' })
    expect(wrapper.find('[data-test="user-credits-history-empty"]').exists()).toBe(true)

    listUserCreditAdjustments.mockRejectedValueOnce(err('unauthorized', 401))
    wrapper = await mountPanel()
    expect(wrapper.emitted('expired')).toHaveLength(1)
  })

  it('needs a whole positive amount and a note before saving', async () => {
    const wrapper = await mountPanel()
    const save = () => wrapper.find('[data-test="user-credits-save"]')
    const preview = () => wrapper.find('[data-test="user-credits-preview"]').text()
    expect(save().attributes('disabled')).toBeDefined()
    expect(preview()).toBe('A whole number above zero.')

    await wrapper.find('[data-test="user-credits-amount"]').setValue('2.5')
    expect(preview()).toBe('A whole number above zero.')
    await wrapper.find('[data-test="user-credits-amount"]').setValue('500')
    expect(preview()).toBe('Balance after: 800')
    expect(save().attributes('disabled')).toBeDefined()
    await wrapper.find('[data-test="user-credits-note"]').setValue('   ')
    expect(save().attributes('disabled')).toBeDefined()
    await wrapper.find('[data-test="user-credits-note"]').setValue('refund')
    expect(save().attributes('disabled')).toBeUndefined()
    expect(save().text()).toBe('Grant credits')

    await wrapper.find('[data-test="user-credits-mode-deduct"]').trigger('click')
    expect(wrapper.find('[data-test="user-credits-mode-deduct"]').attributes('aria-checked')).toBe('true')
    expect(save().text()).toBe('Deduct credits')
    expect(preview()).toBe('At most 300 can be deducted.')
    expect(save().attributes('disabled')).toBeDefined()
    await wrapper.find('[data-test="user-credits-amount"]').setValue('300')
    expect(preview()).toBe('Balance after: 0')
    expect(save().attributes('disabled')).toBeUndefined()
  })

  it('cannot deduct from a balance below zero', async () => {
    const wrapper = await mountPanel({ user: user({ credits: -10, paused: true }) })
    await wrapper.find('[data-test="user-credits-mode-deduct"]').trigger('click')
    await wrapper.find('[data-test="user-credits-amount"]').setValue('1')
    expect(wrapper.find('[data-test="user-credits-preview"]').text()).toBe('At most 0 can be deducted.')
  })

  it('grants, disabling the button while saving, then toasts and resets the form', async () => {
    listUserCreditAdjustments.mockResolvedValueOnce({ adjustments: [adjustment({ id: 'old' })], next: '' })
    const wrapper = await mountPanel()
    await wrapper.find('[data-test="user-credits-amount"]').setValue('1500')
    await wrapper.find('[data-test="user-credits-note"]').setValue('  goodwill ')
    let resolve
    adjustUserCredits.mockReturnValueOnce(new Promise((r) => (resolve = r)))
    await wrapper.find('[data-test="user-credits-form"]').trigger('submit')
    expect(adjustUserCredits).toHaveBeenCalledWith('u1', 1500, 'goodwill')
    expect(wrapper.find('[data-test="user-credits-save"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-test="user-credits-save"]').text()).toBe('Saving...')

    resolve({ user: user({ credits: 1800 }), adjustment: adjustment({ id: 'new', amount: 1500, note: 'goodwill', balanceAfter: 1800 }) })
    await flushPromises()
    expect(wrapper.emitted('updated')[0][0].credits).toBe(1800)
    expect(useToasts().toasts.value.at(-1)).toMatchObject({ type: 'success', message: 'Granted 1,500 credits. Balance: 1,800.' })
    expect(wrapper.findAll('li')[0].attributes('data-test')).toBe('user-credits-entry-new')
    expect(wrapper.findAll('li')).toHaveLength(2)
    expect(wrapper.find('[data-test="user-credits-amount"]').element.value).toBe('')
    expect(wrapper.find('[data-test="user-credits-note"]').element.value).toBe('')
    expect(wrapper.find('[data-test="user-credits-save"]').text()).toBe('Grant credits')
  })

  it('deducts with a negative amount', async () => {
    const wrapper = await mountPanel()
    await wrapper.find('[data-test="user-credits-mode-deduct"]').trigger('click')
    await wrapper.find('[data-test="user-credits-amount"]').setValue('100')
    await wrapper.find('[data-test="user-credits-note"]').setValue('chargeback')
    adjustUserCredits.mockResolvedValueOnce({ user: user({ credits: 200 }), adjustment: adjustment({ amount: -100, balanceAfter: 200 }) })
    await wrapper.find('[data-test="user-credits-form"]').trigger('submit')
    await flushPromises()
    expect(adjustUserCredits).toHaveBeenCalledWith('u1', -100, 'chargeback')
    expect(useToasts().toasts.value.at(-1)).toMatchObject({ type: 'success', message: 'Deducted 100 credits. Balance: 200.' })
  })

  it('keeps the form on a refusal and reports an expired session', async () => {
    const wrapper = await mountPanel()
    await wrapper.find('[data-test="user-credits-amount"]').setValue('5')
    await wrapper.find('[data-test="user-credits-note"]').setValue('x')
    adjustUserCredits.mockRejectedValueOnce(err('the deduction would take the balance below zero', 409))
    await wrapper.find('[data-test="user-credits-form"]').trigger('submit')
    await flushPromises()
    expect(useToasts().toasts.value.at(-1)).toMatchObject({ type: 'error', message: 'the deduction would take the balance below zero' })
    expect(wrapper.find('[data-test="user-credits-note"]').element.value).toBe('x')
    expect(wrapper.find('[data-test="user-credits-save"]').attributes('disabled')).toBeUndefined()

    adjustUserCredits.mockRejectedValueOnce(err('unauthorized', 401))
    await wrapper.find('[data-test="user-credits-form"]').trigger('submit')
    await flushPromises()
    expect(wrapper.emitted('expired')).toHaveLength(1)
  })
})
