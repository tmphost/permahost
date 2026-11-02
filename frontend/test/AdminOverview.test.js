// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

vi.mock('../src/api', () => ({ fetchAdminStats: vi.fn() }))

import AdminOverview from '../src/components/AdminOverview.vue'
import { fetchAdminStats } from '../src/api'

describe('AdminOverview', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('shows the totals and the daily charts', async () => {
    fetchAdminStats.mockResolvedValue({
      users: 1234,
      credits: 56000,
      used: 9,
      signups: 2,
      folders: 7,
      paused: 3,
      days: [
        { date: '2026-10-20', used: 5, signups: 2 },
        { date: '2026-10-21', used: 4, signups: 0 },
      ],
    })
    const wrapper = mount(AdminOverview)
    expect(wrapper.text()).toContain('Loading...')
    await flushPromises()

    const stat = (name) => wrapper.find(`[data-test="admin-stat-${name}"]`).text()
    expect(stat('users')).toBe((1234).toLocaleString())
    expect(stat('credits')).toBe((56000).toLocaleString())
    expect(stat('used')).toBe('9')
    expect(stat('signups')).toBe('2')
    expect(stat('folders')).toBe('7')
    expect(stat('paused')).toBe('3')
    expect(wrapper.find('[data-test="admin-chart-used"]').text()).toContain('Credits used')
    expect(wrapper.find('[data-test="admin-chart-signups"]').text()).toContain('Sign-ups')
    expect(wrapper.text()).not.toContain('Loading...')
  })

  it('reports a failed load and shows zeros', async () => {
    const err = new Error('Failed to load stats (500)')
    fetchAdminStats.mockRejectedValue(err)
    const wrapper = mount(AdminOverview)
    await flushPromises()
    expect(wrapper.emitted('error')).toEqual([[err]])
    expect(wrapper.find('[data-test="admin-stat-users"]').text()).toBe('0')
  })
})
