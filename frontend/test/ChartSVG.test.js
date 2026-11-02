// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ChartSVG from '../src/components/ChartSVG.vue'

const days = (counts) => counts.map((count, i) => ({ date: `2026-10-${String(i + 1).padStart(2, '0')}`, count }))

// hover moves the mouse to x percent of a 100px wide chart.
async function hover(wrapper, x, width = 100) {
  const svg = wrapper.find('[data-test="chart"]')
  svg.element.getBoundingClientRect = () => ({ left: 0, width })
  await svg.trigger('mousemove', { clientX: x })
}

describe('ChartSVG', () => {
  it('shows an empty state without data', async () => {
    const wrapper = mount(ChartSVG)
    expect(wrapper.text()).toContain('No data points')
    expect(wrapper.find('[data-test="chart-line"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="chart-x-label"]').exists()).toBe(false)
    await hover(wrapper, 50)
    expect(wrapper.find('[data-test="chart-tooltip"]').exists()).toBe(false)
  })

  it('draws a flat segment for one point and a straight line for two', () => {
    const one = mount(ChartSVG, { props: { data: days([3]), color: '#aec477' } })
    expect(one.find('[data-test="chart-line"]').attributes('d')).toBe('M 45.00 18.00 L 55.00 18.00')
    expect(one.find('[data-test="chart-line"]').attributes('stroke')).toBe('#aec477')
    expect(one.find('[data-test="chart-max"]').text()).toBe('3')

    const two = mount(ChartSVG, { props: { data: days([0, 0]) } })
    expect(two.find('[data-test="chart-line"]').attributes('d')).toBe('M 5.00 82.00 L 95.00 82.00')
    expect(two.find('[data-test="chart-max"]').text()).toBe('1')
  })

  it('draws a smooth curve with labels for many points', () => {
    const wrapper = mount(ChartSVG, { props: { data: days([0, 5, 1, 9, 2, 8, 3, 7, 4, 6, 0, 1, 2, 3]) } })
    const d = wrapper.find('[data-test="chart-line"]').attributes('d')
    expect(d.startsWith('M 5.00 82.00 C')).toBe(true)
    expect(d.match(/C /g)).toHaveLength(13)
    const labels = wrapper.findAll('[data-test="chart-x-label"]').map((l) => l.text())
    expect(labels).toEqual(['10-01', '10-04', '10-07', '10-10', '10-14'], '10-13 makes way for the last day')
  })

  it('does not crowd the last label next to the previous one', () => {
    const wrapper = mount(ChartSVG, { props: { data: days([1, 2, 3, 4, 5, 6, 7]) } })
    const labels = wrapper.findAll('[data-test="chart-x-label"]').map((l) => l.text())
    expect(labels).toEqual(['10-01', '10-03', '10-05', '10-07'])
    const exact = mount(ChartSVG, { props: { data: days(new Array(31).fill(1)) } })
    expect(exact.findAll('[data-test="chart-x-label"]').at(-1).text()).toBe('10-31')
    const added = mount(ChartSVG, { props: { data: days(new Array(30).fill(1)) } })
    expect(added.findAll('[data-test="chart-x-label"]').map((l) => l.text()).slice(-2)).toEqual(['10-26', '10-30'])
  })

  it('formats large values', () => {
    expect(mount(ChartSVG, { props: { data: days([1500]) } }).find('[data-test="chart-max"]').text()).toBe('1.5k')
    expect(mount(ChartSVG, { props: { data: days([2500000]) } }).find('[data-test="chart-max"]').text()).toBe('2.5M')
    expect(mount(ChartSVG, { props: { data: days([1]) } }).find('[data-test="chart-min"]').text()).toBe('0')
  })

  it('starts the axis below zero for a negative balance', () => {
    const wrapper = mount(ChartSVG, { props: { data: days([-1500, 500]) } })
    expect(wrapper.find('[data-test="chart-min"]').text()).toBe('-1.5k')
    expect(wrapper.find('[data-test="chart-max"]').text()).toBe('500')
    expect(wrapper.find('[data-test="chart-line"]').attributes('d')).toBe('M 5.00 82.00 L 95.00 18.00')
    expect(mount(ChartSVG, { props: { data: days([-2500000, -5]) } }).find('[data-test="chart-min"]').text()).toBe('-2.5M')
    expect(mount(ChartSVG, { props: { data: days([-5]) } }).find('[data-test="chart-min"]').text()).toBe('-5')
  })

  it('shows the nearest point on hover and hides it on leave', async () => {
    const wrapper = mount(ChartSVG, { props: { data: days([2, 4, 1]) } })
    await hover(wrapper, 52)
    expect(wrapper.find('[data-test="chart-tooltip"]').text()).toBe('10-02: 4')
    expect(wrapper.find('[data-test="chart-tooltip"]').attributes('style')).toContain('translateX(-50%) translateY(10%)')
    await hover(wrapper, 0)
    expect(wrapper.find('[data-test="chart-tooltip"]').attributes('style')).toContain('translateX(0) translateY(-110%)')
    await hover(wrapper, 100, 0)
    expect(wrapper.find('[data-test="chart-tooltip"]').attributes('style')).toContain('translateX(-100%)')
    await wrapper.find('[data-test="chart"]').trigger('mouseleave')
    expect(wrapper.find('[data-test="chart-tooltip"]').exists()).toBe(false)
  })
})
