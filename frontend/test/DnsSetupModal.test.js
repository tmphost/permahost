// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import DnsSetupModal from '../src/components/DnsSetupModal.vue'
import { useToasts } from '../src/composables/useToasts'

const domain = { id: 'd1', hostname: 'www.example.com', target: 'fabc.permahost.io', status: 'pending' }
const pricing = { domainSetupCredits: 700, domainCredits: 500 }

describe('DnsSetupModal', () => {
  let writeText
  beforeEach(() => {
    useToasts().toasts.value = []
    writeText = vi.fn().mockResolvedValue()
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
  })
  afterEach(() => {
    delete navigator.clipboard
    document.body.innerHTML = ''
  })

  it('renders nothing without a domain', () => {
    expect(mount(DnsSetupModal).find('[data-test="dns-modal"]').exists()).toBe(false)
  })

  it('shows the record to create, focused, and copies its parts', async () => {
    const wrapper = mount(DnsSetupModal, { attachTo: document.body, props: { pricing } })
    await wrapper.setProps({ domain })
    await flushPromises()
    expect(document.activeElement).toBe(wrapper.find('[role="dialog"]').element)
    expect(wrapper.find('#dns-modal-title').text()).toBe('Point www.example.com at this folder')
    expect(wrapper.text()).toContain('where example.com is managed')
    expect(wrapper.find('[data-test="dns-name"]').text()).toBe('www')
    expect(wrapper.find('[data-test="dns-target"]').text()).toBe('fabc.permahost.io')
    expect(wrapper.find('[data-test="dns-tips"]').text()).toContain('DNS only')
    expect(wrapper.find('[data-test="dns-apex"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="dns-pricing"]').text()).toBe(
      'Nothing is charged until the record is verified. Then it costs 700 credits to set up and 500 a month.',
    )

    await wrapper.find('[data-test="dns-copy-name"]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenLastCalledWith('www')
    await wrapper.find('[data-test="dns-copy-target"]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenLastCalledWith('fabc.permahost.io')
    expect(useToasts().toasts.value.at(-1).message).toBe('Target copied')

    // Closing again leaves nothing to focus.
    await wrapper.setProps({ domain: null })
    expect(wrapper.find('[data-test="dns-modal"]').exists()).toBe(false)
  })

  it('emits verify and close, and locks while verifying', async () => {
    const wrapper = mount(DnsSetupModal, { props: { domain } })
    expect(wrapper.find('[data-test="dns-pricing"]').exists()).toBe(false)
    await wrapper.find('[data-test="dns-verify"]').trigger('click')
    expect(wrapper.emitted('verify')).toHaveLength(1)

    await wrapper.find('[data-test="dns-close"]').trigger('click')
    await wrapper.find('[data-test="dns-modal"]').trigger('click')
    await wrapper.find('[data-test="dns-modal"]').trigger('keydown', { key: 'Escape' })
    await wrapper.find('[role="dialog"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(3)

    await wrapper.setProps({ verifying: true })
    expect(wrapper.find('[data-test="dns-verify"]').text()).toBe('Checking…')
    expect(wrapper.find('[data-test="dns-verify"]').attributes('disabled')).toBeDefined()
    await wrapper.find('[data-test="dns-modal"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(3)
  })

  it('explains that a bare domain cannot have a CNAME', () => {
    const wrapper = mount(DnsSetupModal, { props: { domain: { ...domain, hostname: 'example.com' } } })
    expect(wrapper.find('[data-test="dns-apex"]').text()).toContain('add a subdomain such as www.example.com instead')
    expect(wrapper.find('[data-test="dns-record"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="dns-tips"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="dns-verify"]').exists()).toBe(false)
  })
})
