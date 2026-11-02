// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

vi.mock('../src/api', () => ({
  listDomains: vi.fn(),
  createDomain: vi.fn(),
  verifyDomain: vi.fn(),
  deleteDomain: vi.fn(),
}))

import { createDomain, deleteDomain, listDomains, verifyDomain } from '../src/api'
import FolderDomains from '../src/components/FolderDomains.vue'
import { useToasts } from '../src/composables/useToasts'

const folder = { id: 'f1' }
const pricing = { domainCredits: 500, domainSetupCredits: 700 }
const pending = { id: 'd1', folderId: 'f1', hostname: 'www.example.com', target: 'ff1.permahost.test', status: 'pending' }
const active = { id: 'd2', folderId: 'f1', hostname: 'docs.example.com', target: 'ff1.permahost.test', status: 'active' }

function deferred() {
  let resolve
  const promise = new Promise((r) => (resolve = r))
  return { promise, resolve }
}

async function mountPanel(props = {}) {
  const wrapper = mount(FolderDomains, { attachTo: document.body, props: { folder, pricing, ...props } })
  await flushPromises()
  return wrapper
}

const lastToast = () => useToasts().toasts.value.at(-1)

describe('FolderDomains', () => {
  let writeText
  beforeEach(() => {
    useToasts().toasts.value = []
    listDomains.mockResolvedValue([pending, active])
    writeText = vi.fn().mockResolvedValue()
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
  })
  afterEach(() => {
    vi.clearAllMocks()
    useToasts().toasts.value = []
    delete navigator.clipboard
    document.body.innerHTML = ''
  })

  it('loads the domains with their status and the CNAME to create', async () => {
    const slow = deferred()
    listDomains.mockReturnValueOnce(slow.promise)
    const wrapper = mount(FolderDomains, { props: { folder, pricing } })
    expect(wrapper.text()).toContain('Loading domains…')
    slow.resolve([{ ...pending, lastError: 'www.example.com has no CNAME record' }, active])
    await flushPromises()
    expect(listDomains).toHaveBeenCalledWith('f1')

    const row = wrapper.find('[data-test="domain-www.example.com"]')
    expect(row.find('[data-test="domain-status"]').text()).toBe('Pending')
    expect(row.find('[data-test="domain-link"]').exists()).toBe(false)
    expect(row.find('[data-test="domain-target"]').text()).toBe('ff1.permahost.test')
    expect(row.find('[data-test="domain-error"]').text()).toBe('www.example.com has no CNAME record')
    expect(row.find('[data-test="domain-verify"]').text()).toBe('Verify now')

    const live = wrapper.find('[data-test="domain-docs.example.com"]')
    expect(live.find('[data-test="domain-status"]').text()).toBe('Active')
    expect(live.find('[data-test="domain-link"]').attributes('href')).toBe('https://docs.example.com/')
    expect(live.find('[data-test="domain-verify"]').exists()).toBe(false)
    expect(live.find('[data-test="domain-target"]').exists()).toBe(false)

    expect(wrapper.find('[data-test="domain-pricing"]').text()).toBe(
      'Domains are checked every few minutes. 700 credits to set up and 500 a month, charged once the CNAME is verified.',
    )
  })

  it('toasts load errors and leaves out the price until it is known', async () => {
    listDomains.mockRejectedValueOnce(new Error('Failed to load domains (500)'))
    const wrapper = await mountPanel({ pricing: null })
    expect(lastToast()).toMatchObject({ type: 'error', message: 'Failed to load domains (500)' })
    expect(wrapper.find('[data-test^="domain-www"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="domain-pricing"]').text()).toBe('Domains are checked every few minutes.')
  })

  it('copies the CNAME target', async () => {
    const wrapper = await mountPanel()
    await wrapper.find('[data-test="domain-copy"]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith('ff1.permahost.test')
    expect(lastToast()).toMatchObject({ type: 'success', message: 'CNAME target copied' })
  })

  it('adds a domain', async () => {
    listDomains.mockResolvedValueOnce([])
    const wrapper = await mountPanel()
    const add = () => wrapper.find('[data-test="domain-add"]')
    expect(add().attributes('disabled')).toBeDefined()

    // A blank name is not sent.
    await wrapper.find('[data-test="domain-input"]').setValue('   ')
    await wrapper.find('[data-test="domain-form"]').trigger('submit')
    expect(createDomain).not.toHaveBeenCalled()

    const slow = deferred()
    createDomain.mockReturnValueOnce(slow.promise)
    await wrapper.find('[data-test="domain-input"]').setValue(' www.example.com ')
    expect(add().attributes('disabled')).toBeUndefined()
    await wrapper.find('[data-test="domain-form"]').trigger('submit')
    expect(createDomain).toHaveBeenCalledWith('f1', 'www.example.com')
    expect(add().attributes('disabled')).toBeDefined()
    slow.resolve(pending)
    await flushPromises()
    expect(wrapper.find('[data-test="domain-www.example.com"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="domain-input"]').element.value).toBe('')
    expect(lastToast()).toMatchObject({ type: 'success', message: 'Added www.example.com. Point its CNAME at the target, then verify.' })
    expect(wrapper.emitted('changed')).toBeUndefined()
  })

  it('toasts a domain that cannot be added', async () => {
    createDomain.mockRejectedValueOnce(new Error('www.example.com is already in use'))
    const wrapper = await mountPanel()
    await wrapper.find('[data-test="domain-input"]').setValue('www.example.com')
    await wrapper.find('[data-test="domain-form"]').trigger('submit')
    await flushPromises()
    expect(lastToast()).toMatchObject({ type: 'error', message: 'www.example.com is already in use' })
    expect(wrapper.find('[data-test="domain-input"]').element.value).toBe('www.example.com')
    expect(wrapper.find('[data-test="domain-add"]').attributes('disabled')).toBeUndefined()
  })

  it('verifies a domain, which charges for it', async () => {
    const slow = deferred()
    verifyDomain.mockReturnValueOnce(slow.promise)
    const wrapper = await mountPanel()
    await wrapper.find('[data-test="domain-verify"]').trigger('click')
    expect(verifyDomain).toHaveBeenCalledWith('f1', 'd1')
    expect(wrapper.find('[data-test="domain-verify"]').text()).toBe('Checking…')
    expect(wrapper.find('[data-test="domain-verify"]').attributes('disabled')).toBeDefined()

    slow.resolve({ ...pending, status: 'active' })
    await flushPromises()
    const row = wrapper.find('[data-test="domain-www.example.com"]')
    expect(row.find('[data-test="domain-status"]').text()).toBe('Active')
    expect(row.find('[data-test="domain-verify"]').exists()).toBe(false)
    expect(lastToast()).toMatchObject({
      type: 'success',
      message: 'www.example.com is active. Its certificate is issued within a minute.',
    })
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  it('shows why a verification failed', async () => {
    verifyDomain.mockRejectedValueOnce(Object.assign(new Error('the CNAME record is not set up yet'), { status: 422 }))
    const wrapper = await mountPanel()
    listDomains.mockResolvedValueOnce([{ ...pending, lastError: 'www.example.com points at elsewhere.test' }, active])
    await wrapper.find('[data-test="domain-verify"]').trigger('click')
    await flushPromises()
    expect(lastToast()).toMatchObject({ type: 'error', message: 'the CNAME record is not set up yet' })
    expect(wrapper.find('[data-test="domain-error"]').text()).toBe('www.example.com points at elsewhere.test')
    expect(wrapper.find('[data-test="domain-verify"]').text()).toBe('Verify now')
    expect(wrapper.emitted('changed')).toBeUndefined()
  })

  it('removes a domain after confirmation', async () => {
    deleteDomain.mockResolvedValue()
    const wrapper = await mountPanel()
    const remove = () => wrapper.find('[data-test="domain-docs.example.com"] [data-test="domain-delete"]')
    await remove().trigger('click')
    const modal = wrapper.find('[data-test="delete-modal"]')
    expect(modal.text()).toContain('Remove domain?')
    expect(modal.text()).toContain('docs.example.com stops serving this folder, and its monthly charge stops.')
    expect(wrapper.find('[data-test="delete-modal-confirm"]').text()).toBe('Remove')

    await wrapper.find('[data-test="delete-modal-cancel"]').trigger('click')
    expect(wrapper.find('[data-test="delete-modal"]').exists()).toBe(false)

    await remove().trigger('click')
    await wrapper.find('[data-test="delete-modal-confirm"]').trigger('click')
    await flushPromises()
    expect(deleteDomain).toHaveBeenCalledWith('f1', 'd2')
    expect(wrapper.find('[data-test="domain-docs.example.com"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="delete-modal"]').exists()).toBe(false)
    expect(lastToast()).toMatchObject({ type: 'success', message: 'Domain removed' })
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  it('toasts a domain that cannot be removed', async () => {
    deleteDomain.mockRejectedValueOnce(new Error('Failed to remove domain (500)'))
    const wrapper = await mountPanel()
    await wrapper.find('[data-test="domain-www.example.com"] [data-test="domain-delete"]').trigger('click')
    await wrapper.find('[data-test="delete-modal-confirm"]').trigger('click')
    await flushPromises()
    expect(lastToast()).toMatchObject({ type: 'error', message: 'Failed to remove domain (500)' })
    expect(wrapper.find('[data-test="domain-www.example.com"]').exists()).toBe(true)
    expect(wrapper.emitted('changed')).toBeUndefined()
  })
})
