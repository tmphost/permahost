// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import DeleteModal from '../src/components/DeleteModal.vue'

describe('DeleteModal', () => {
  it('renders nothing while closed', () => {
    expect(mount(DeleteModal).find('[data-test="delete-modal"]').exists()).toBe(false)
  })

  it('shows defaults and emits confirm and cancel', async () => {
    const wrapper = mount(DeleteModal, { props: { open: true } })
    expect(wrapper.text()).toContain('Delete?')
    expect(wrapper.find('[data-test="delete-modal-message"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="delete-modal-confirm"]').text()).toBe('Delete')

    await wrapper.find('[data-test="delete-modal-confirm"]').trigger('click')
    expect(wrapper.emitted('confirm')).toHaveLength(1)
    await wrapper.find('[data-test="delete-modal-cancel"]').trigger('click')
    await wrapper.find('[data-test="delete-modal"]').trigger('click')
    await wrapper.find('[data-test="delete-modal"]').trigger('keydown', { key: 'Escape' })
    expect(wrapper.emitted('cancel')).toHaveLength(3)
  })

  it('ignores clicks inside the dialog', async () => {
    const wrapper = mount(DeleteModal, { props: { open: true } })
    await wrapper.find('[role="alertdialog"]').trigger('click')
    expect(wrapper.emitted('cancel')).toBeUndefined()
  })

  it('shows custom text and locks while busy', async () => {
    const wrapper = mount(DeleteModal, {
      props: { open: true, title: 'Delete folder?', message: 'Gone for good.', confirmLabel: 'Purge', busy: true },
    })
    expect(wrapper.text()).toContain('Delete folder?')
    expect(wrapper.find('[data-test="delete-modal-message"]').text()).toBe('Gone for good.')
    expect(wrapper.find('[data-test="delete-modal-confirm"]').text()).toBe('Deleting…')
    expect(wrapper.find('[data-test="delete-modal-confirm"]').attributes('disabled')).toBeDefined()
    await wrapper.find('[data-test="delete-modal"]').trigger('click')
    expect(wrapper.emitted('cancel')).toBeUndefined()

    await wrapper.setProps({ busy: false })
    expect(wrapper.find('[data-test="delete-modal-confirm"]').text()).toBe('Purge')
  })
})
