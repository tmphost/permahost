// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

vi.mock('../src/api', () => ({ grantCredits: vi.fn() }))

import { grantCredits } from '../src/api'
import AdminGrant from '../src/components/AdminGrant.vue'
import { useToasts } from '../src/composables/useToasts'

const EXPIRES = '2026-12-03T00:00:00Z'

function expiresText() {
  return new Date(EXPIRES).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
}

describe('AdminGrant', () => {
  afterEach(() => {
    vi.clearAllMocks()
    useToasts().toasts.value.splice(0)
  })

  const submit = (wrapper) => wrapper.find('[data-test="admin-grant-submit"]')

  it('grants to every user, disabling the button while it runs', async () => {
    const wrapper = mount(AdminGrant)
    expect(submit(wrapper).text()).toBe('Grant to all users')
    expect(wrapper.find('[data-test="admin-email"]').exists()).toBe(false)
    expect(submit(wrapper).attributes('disabled'), 'a note is required').toBeDefined()

    await wrapper.find('[data-test="admin-credits"]').setValue('2500')
    await wrapper.find('[data-test="admin-grant-note"]').setValue('  launch bonus ')
    expect(submit(wrapper).attributes('disabled')).toBeUndefined()

    let resolve
    grantCredits.mockReturnValueOnce(new Promise((r) => (resolve = r)))
    await wrapper.find('[data-test="admin-grant"]').trigger('submit')
    expect(grantCredits).toHaveBeenCalledWith({ credits: 2500, email: '', all: true, note: 'launch bonus' })
    expect(submit(wrapper).text()).toBe('Granting...')
    expect(submit(wrapper).attributes('disabled')).toBeDefined()

    resolve({ users: 1200, expiresAt: EXPIRES })
    await flushPromises()
    expect(wrapper.find('[data-test="admin-grant-result"]').text()).toBe(
      `Granted ${(2500).toLocaleString()} credits to ${(1200).toLocaleString()} users. They expire ${expiresText()}.`,
    )
    expect(useToasts().toasts.value.at(-1)).toMatchObject({
      type: 'success',
      message: `Granted ${(2500).toLocaleString()} credits to ${(1200).toLocaleString()} users.`,
    })
    expect(wrapper.find('[data-test="admin-grant-note"]').element.value).toBe('')
  })

  it('grants to one user by email', async () => {
    const wrapper = mount(AdminGrant)
    await wrapper.find('[data-test="admin-target-one"]').trigger('click')
    expect(wrapper.find('[data-test="admin-target-one"]').attributes('aria-checked')).toBe('true')
    expect(submit(wrapper).text()).toBe('Grant')
    await wrapper.find('[data-test="admin-grant-note"]').setValue('support')
    expect(submit(wrapper).attributes('disabled'), 'an email is required').toBeDefined()
    await wrapper.find('[data-test="admin-email"]').setValue(' ada@x.dev ')

    grantCredits.mockResolvedValueOnce({ users: 1, expiresAt: EXPIRES })
    await wrapper.find('[data-test="admin-grant"]').trigger('submit')
    await flushPromises()
    expect(grantCredits).toHaveBeenCalledWith({ credits: 100, email: 'ada@x.dev', all: false, note: 'support' })
    expect(wrapper.find('[data-test="admin-grant-result"]').text()).toBe(`Granted 100 credits to ada@x.dev. They expire ${expiresText()}.`)
    expect(useToasts().toasts.value.at(-1).message).toBe('Granted 100 credits to 1 user.')
  })

  it('refuses amounts outside 1 to 1,000,000', async () => {
    const wrapper = mount(AdminGrant)
    await wrapper.find('[data-test="admin-grant-note"]').setValue('x')
    for (const v of ['0', '1000001', '2.5']) {
      await wrapper.find('[data-test="admin-credits"]').setValue(v)
      expect(submit(wrapper).attributes('disabled'), v).toBeDefined()
    }
    await wrapper.find('[data-test="admin-credits"]').setValue('1000000')
    expect(submit(wrapper).attributes('disabled')).toBeUndefined()
  })

  it('shows a refused grant and clears the last result', async () => {
    const wrapper = mount(AdminGrant)
    await wrapper.find('[data-test="admin-grant-note"]').setValue('x')
    grantCredits.mockResolvedValueOnce({ users: 2, expiresAt: EXPIRES })
    await wrapper.find('[data-test="admin-grant"]').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[data-test="admin-grant-result"]').text()).toContain('to 2 users')

    await wrapper.find('[data-test="admin-grant-note"]').setValue('x')
    grantCredits.mockRejectedValueOnce(Object.assign(new Error('user not found'), { status: 404 }))
    await wrapper.find('[data-test="admin-grant"]').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[data-test="admin-grant-error"]').text()).toBe('user not found')
    expect(wrapper.find('[data-test="admin-grant-result"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="admin-grant-note"]').element.value).toBe('x', 'kept for a retry')
    expect(wrapper.emitted('error')).toBeUndefined()
  })

  it('hands an ended root session to the page', async () => {
    const wrapper = mount(AdminGrant)
    await wrapper.find('[data-test="admin-grant-note"]').setValue('x')
    const err = Object.assign(new Error('unauthorized'), { status: 401 })
    grantCredits.mockRejectedValueOnce(err)
    await wrapper.find('[data-test="admin-grant"]').trigger('submit')
    await flushPromises()
    expect(wrapper.emitted('error')).toEqual([[err]])
    expect(wrapper.find('[data-test="admin-grant-error"]').exists()).toBe(false)
    expect(submit(wrapper).text()).toBe('Grant to all users')
  })
})
