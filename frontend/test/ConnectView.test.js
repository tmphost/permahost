// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'

vi.mock('../src/api', () => ({ BASE_PATH: '/base' }))

import ConnectView from '../src/views/ConnectView.vue'
import { useToasts } from '../src/composables/useToasts'

const lastToast = () => useToasts().toasts.value.at(-1)

async function mountView(path = '/connect') {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/connect', component: ConnectView }] })
  await router.push(path)
  const wrapper = mount(ConnectView, { global: { plugins: [router] } })
  return { wrapper, router }
}

describe('ConnectView', () => {
  let writeText
  beforeEach(() => {
    useToasts().toasts.value = []
    writeText = vi.fn().mockResolvedValue()
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
  })
  afterEach(() => {
    useToasts().toasts.value = []
    delete navigator.clipboard
  })

  const server = () => `${window.location.origin}/base`
  const mcpUrl = () => `${server()}/mcp`

  it('explains MCP setup with the server URL, JSON config and Claude Code command', async () => {
    const { wrapper } = await mountView()
    expect(wrapper.find('h1').text()).toBe('Connect')
    expect(wrapper.find('[data-test="snippet-mcp-url"]').text()).toBe(mcpUrl())
    expect(JSON.parse(wrapper.find('[data-test="snippet-mcp-json"]').text())).toEqual({
      mcpServers: { tmphost: { type: 'http', url: mcpUrl() } },
    })
    expect(wrapper.find('[data-test="snippet-mcp-claude"]').text()).toBe(`claude mcp add --transport http tmphost ${mcpUrl()}`)
    expect(wrapper.find('[data-test="connect-mcp-note"]').text()).toContain('consent screen')
    expect(wrapper.find('[data-test="connect-mcp-note"]').text()).toContain('No secret or key is needed')
    const tool = wrapper.find('[data-test="connect-mcp-tool"]').text()
    for (const word of ['file', 'upload', 'download', 'get', 'url']) expect(tool).toContain(word)
    expect(wrapper.find('[data-test="connect-mcp"]').attributes('aria-labelledby')).toBe('connect-mcp-title')
    expect(wrapper.find('#connect-mcp-title').text()).toBe('MCP clients')
  })

  it('explains CLI install, login and usage', async () => {
    const { wrapper } = await mountView('/connect?tab=cli')
    expect(wrapper.find('[data-test="snippet-cli-install-unix"]').text()).toBe(
      'curl -fsSL https://raw.githubusercontent.com/tmphost/tmphost/main/install.sh | sh',
    )
    expect(wrapper.find('[data-test="snippet-cli-install-windows"]').text()).toBe(
      'irm https://raw.githubusercontent.com/tmphost/tmphost/main/install.ps1 | iex',
    )
    expect(wrapper.find('[data-test="snippet-cli-login"]').text()).toBe(`tmphost login --server ${server()}`)
    expect(wrapper.find('[data-test="snippet-cli-examples"]').text().split('\n')).toEqual([
      'tmphost file upload ./index.html --ttl 1h',
      'tmphost file get --id <id>',
      'tmphost file url --id <id>',
    ])
    expect(wrapper.find('[data-test="connect-cli"] [data-test="connect-mcp-tool"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="connect-cli-note"]').text()).toContain('browser')
  })

  it('switches between MCP and CLI through the route', async () => {
    const { wrapper, router } = await mountView('/connect?tab=nope&x=1')
    const mcpTab = wrapper.find('[data-test="connect-tab-mcp"]')
    const cliTab = wrapper.find('[data-test="connect-tab-cli"]')
    expect(wrapper.find('nav').attributes('aria-label')).toBe('Connect with')
    expect(mcpTab.text()).toBe('MCP')
    expect(cliTab.text()).toBe('CLI')
    expect(mcpTab.attributes('aria-current')).toBe('page')
    expect(cliTab.attributes('aria-current')).toBeUndefined()
    expect(wrapper.find('[data-test="connect-mcp"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="connect-cli"]').exists()).toBe(false)

    await cliTab.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ tab: 'cli', x: '1' })
    expect(wrapper.find('[data-test="connect-cli"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="connect-mcp"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="connect-tab-cli"]').attributes('aria-current')).toBe('page')
    expect(wrapper.find('[data-test="connect-tab-cli"]').classes()).toContain('font-semibold')

    await wrapper.find('[data-test="connect-tab-mcp"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.tab).toBe('mcp')
    expect(wrapper.find('[data-test="connect-mcp"]').exists()).toBe(true)
  })

  it('places an icon copy button inside each block', async () => {
    const { wrapper } = await mountView()
    const single = wrapper.find('[data-test="copy-mcp-url"]')
    expect(single.text()).toBe('')
    expect(single.find('svg').exists()).toBe(true)
    expect(single.attributes('aria-label')).toBe('Copy MCP server URL')
    expect(single.attributes('title')).toBe('Copy MCP server URL')
    expect(single.classes()).toContain('-translate-y-1/2')
    const multi = wrapper.find('[data-test="copy-mcp-json"]')
    expect(multi.classes()).toContain('top-1.5')
    expect(multi.classes()).not.toContain('-translate-y-1/2')
    expect(wrapper.find('[data-test="snippet-mcp-url"]').classes()).toContain('pr-11')
  })

  it('copies a snippet and briefly shows a check mark', async () => {
    vi.useFakeTimers()
    try {
      const { wrapper } = await mountView()
      const copy = wrapper.find('[data-test="copy-mcp-url"]')
      const icon = () => copy.find('path').attributes('d')
      const copyIcon = icon()
      await copy.trigger('click')
      await flushPromises()
      expect(writeText).toHaveBeenLastCalledWith(mcpUrl())
      expect(lastToast()).toMatchObject({ type: 'success', message: 'MCP server URL copied' })
      expect(icon()).toBe('m5 13 4 4L19 7')
      expect(copy.attributes('title')).toBe('Copied')

      // Copying another snippet moves the check mark.
      await wrapper.find('[data-test="copy-mcp-json"]').trigger('click')
      await flushPromises()
      expect(icon()).toBe(copyIcon)
      expect(wrapper.find('[data-test="copy-mcp-json"]').attributes('title')).toBe('Copied')

      vi.advanceTimersByTime(1500)
      await flushPromises()
      expect(wrapper.find('[data-test="copy-mcp-json"]').attributes('title')).toBe('Copy JSON config')
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it('keeps the copy icon when copying fails', async () => {
    writeText.mockRejectedValue(new Error('denied'))
    const { wrapper } = await mountView()
    const copy = wrapper.find('[data-test="copy-mcp-url"]')
    await copy.trigger('click')
    await flushPromises()
    expect(lastToast()).toMatchObject({ type: 'error', message: 'Could not copy to the clipboard' })
    expect(copy.attributes('title')).toBe('Copy MCP server URL')
  })

  it('copies each snippet', async () => {
    const { wrapper } = await mountView()

    await wrapper.find('[data-test="connect-tab-cli"]').trigger('click')
    await flushPromises()
    await wrapper.find('[data-test="copy-cli-login"]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenLastCalledWith(`tmphost login --server ${server()}`)
    expect(lastToast()).toMatchObject({ type: 'success', message: 'Sign in (opens the browser) copied' })
  })
})
