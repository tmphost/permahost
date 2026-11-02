// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'

vi.mock('../src/api', () => ({
  fetchLimits: vi.fn(),
  createFolder: vi.fn(),
  uploadFile: vi.fn(),
}))

import { createFolder, fetchLimits, uploadFile } from '../src/api'
import UploadView from '../src/views/UploadView.vue'
import { useToasts } from '../src/composables/useToasts'
import { dataTransfer, dirEntry, fileEntry } from './dropEntries'

const limits = {
  defaultTtlMinutes: 10,
  maxTtlMinutes: 10,
  maxUploadBytes: 4 * 1024 * 1024,
  billingBlockMinutes: 30,
  anonymous: true,
  fileOps: 5,
  fileOpsWindowMinutes: 30,
}
const folder = { id: 'abc', url: 'https://tmphost.io/files/1/abc/' }

function file(name, size = 10, webkitRelativePath = '') {
  return { name, size, webkitRelativePath }
}

async function pick(wrapper, selector, list) {
  const input = wrapper.find(selector)
  Object.defineProperty(input.element, 'files', { value: list, configurable: true })
  await input.trigger('change')
}

async function mountView() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/upload', component: UploadView },
      { path: '/login', component: { render: () => null } },
    ],
  })
  await router.push('/upload')
  const wrapper = mount(UploadView, { attachTo: document.body, global: { plugins: [router] } })
  await flushPromises()
  return wrapper
}

const lastToast = () => useToasts().toasts.value.at(-1)

describe('UploadView', () => {
  let writeText
  beforeEach(() => {
    useToasts().toasts.value = []
    fetchLimits.mockResolvedValue(limits)
    createFolder.mockResolvedValue(folder)
    uploadFile.mockImplementation(async (_id, path, f) => ({
      file: { path, size: f.size, url: `${folder.url}${path}` },
      creditsSpent: 0,
    }))
    writeText = vi.fn().mockResolvedValue()
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
  })
  afterEach(() => {
    vi.clearAllMocks()
    useToasts().toasts.value = []
    delete navigator.clipboard
    document.body.innerHTML = ''
  })

  it('shows the anonymous limits and links to sign-in', async () => {
    const wrapper = await mountView()
    expect(wrapper.find('[data-test="upload-limits"]').text()).toBe(
      'Up to 4.0 MB per file and 5 files every 30 min. Links expire after 10 min.',
    )
    expect(wrapper.find('[data-test="upload-sign-in"]').attributes('href')).toBe('/login')
    expect(wrapper.find('[data-test="upload-summary"]').exists()).toBe(false)
  })

  it('reports a limits failure', async () => {
    fetchLimits.mockRejectedValue(new Error('Failed to load limits (401)'))
    const wrapper = await mountView()
    expect(wrapper.find('[data-test="upload-limits"]').exists()).toBe(false)
    expect(lastToast()).toMatchObject({ type: 'error', message: 'Failed to load limits (401)' })
    await pick(wrapper, '[data-test="pick-files"]', [file('a.txt')])
    expect(wrapper.find('[data-test="upload-start"]').attributes('disabled')).toBeDefined()
  })

  it('uploads files into a new folder and lists their links', async () => {
    const wrapper = await mountView()
    await pick(wrapper, '[data-test="pick-files"]', [file('a.txt', 3)])
    await pick(wrapper, '[data-test="pick-folder"]', [file('index.html', 5, 'site/index.html'), file('a.txt', 4)])
    expect(wrapper.findAll('[data-test^="selected-"]').filter((w) => w.element.tagName === 'LI')).toHaveLength(2)

    await wrapper.find('[data-test="upload-start"]').trigger('click')
    await flushPromises()
    expect(createFolder).toHaveBeenCalledWith(10)
    expect(uploadFile.mock.calls.map((c) => c[1])).toEqual(['a.txt', 'site/index.html'])
    expect(lastToast()).toMatchObject({ type: 'success', message: 'Uploaded 2 files' })
    expect(wrapper.find('[data-test="upload-summary"]').exists()).toBe(false)

    const result = wrapper.find('[data-test="upload-result"]')
    expect(result.text()).toContain(folder.url)
    expect(result.find('[data-test="result-site/index.html"]').text()).toContain('5 B')
    await result.find('[data-test="result-folder-copy"]').trigger('click')
    expect(writeText).toHaveBeenCalledWith(folder.url)
    await result.find('[data-test="result-a.txt"] [data-test="result-file-copy"]').trigger('click')
    expect(writeText).toHaveBeenCalledWith(`${folder.url}a.txt`)
  })

  it('says "1 file" and shows progress while uploading', async () => {
    let release
    uploadFile.mockImplementation(
      () => new Promise((r) => (release = () => r({ file: { path: 'a.txt', size: 1, url: 'u' } }))),
    )
    const wrapper = await mountView()
    await pick(wrapper, '[data-test="pick-files"]', [file('a.txt', 1)])
    await wrapper.find('[data-test="upload-start"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="upload-start"]').text()).toBe('Uploading 0 / 1')
    expect(wrapper.find('[data-test="upload-clear"]').attributes('disabled')).toBeDefined()
    release()
    await flushPromises()
    expect(lastToast()).toMatchObject({ type: 'success', message: 'Uploaded 1 file' })
  })

  it('keeps what was uploaded when the quota runs out', async () => {
    uploadFile
      .mockResolvedValueOnce({ file: { path: 'a.txt', size: 1, url: 'u/a.txt' } })
      .mockRejectedValueOnce(new Error('anonymous quota exceeded: at most 5 files per 30 minutes'))
    const wrapper = await mountView()
    await pick(wrapper, '[data-test="pick-files"]', [file('a.txt', 1), file('b.txt', 1)])
    await wrapper.find('[data-test="upload-start"]').trigger('click')
    await flushPromises()
    expect(lastToast()).toMatchObject({ type: 'error', message: 'anonymous quota exceeded: at most 5 files per 30 minutes' })
    expect(wrapper.find('[data-test="result-a.txt"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="result-b.txt"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="selected-b.txt"]').exists()).toBe(true)
  })

  it('adds to the last folder, replacing files with the same path', async () => {
    const wrapper = await mountView()
    await pick(wrapper, '[data-test="pick-files"]', [file('a.txt'), file('b.txt')])
    expect(wrapper.find('[data-test="upload-into-result"]').exists()).toBe(false)
    await wrapper.find('[data-test="upload-start"]').trigger('click')
    await flushPromises()

    await pick(wrapper, '[data-test="pick-files"]', [file('b.txt', 20), file('c.txt')])
    expect(wrapper.find('[data-test="upload-into-result"]').element.checked).toBe(true)
    expect(wrapper.find('[data-test="selected-b.txt"] [data-test="selected-replaces"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="selected-c.txt"] [data-test="selected-replaces"]').exists()).toBe(false)
    await wrapper.find('[data-test="upload-start"]').trigger('click')
    await flushPromises()
    expect(createFolder).toHaveBeenCalledTimes(1)
    expect(uploadFile.mock.calls.slice(2).map((c) => [c[0], c[1]])).toEqual([
      ['abc', 'b.txt'],
      ['abc', 'c.txt'],
    ])
    const rows = wrapper.findAll('[data-test^="result-"]:not([data-test^="result-folder"]):not([data-test="result-file-copy"])')
    expect(rows.map((r) => r.attributes('data-test'))).toEqual(['result-a.txt', 'result-b.txt', 'result-c.txt'])
    expect(wrapper.find('[data-test="result-b.txt"]').text()).toContain('20 B')

    // Unticked, the next upload starts a new folder.
    createFolder.mockResolvedValueOnce({ id: 'xyz', url: 'https://tmphost.io/files/1/xyz/' })
    await pick(wrapper, '[data-test="pick-files"]', [file('a.txt')])
    await wrapper.find('[data-test="upload-into-result"]').setValue(false)
    expect(wrapper.find('[data-test="selected-replaces"]').exists()).toBe(false)
    await wrapper.find('[data-test="upload-start"]').trigger('click')
    await flushPromises()
    expect(createFolder).toHaveBeenCalledTimes(2)
    expect(uploadFile).toHaveBeenLastCalledWith('xyz', 'a.txt', expect.anything())
    expect(wrapper.find('[data-test="result-b.txt"]').exists()).toBe(false)
  })

  it('blocks oversized files and too many files', async () => {
    const wrapper = await mountView()
    await pick(wrapper, '[data-test="pick-files"]', [file('big.bin', limits.maxUploadBytes + 1)])
    expect(wrapper.find('[data-test="upload-oversized"]').text()).toContain('1 file is over the 4.0 MB limit')
    expect(wrapper.find('[data-test="upload-start"]').attributes('disabled')).toBeDefined()
    await pick(wrapper, '[data-test="pick-files"]', [file('big2.bin', limits.maxUploadBytes + 1)])
    expect(wrapper.find('[data-test="upload-oversized"]').text()).toContain('2 files are over')

    await wrapper.find('[data-test="upload-clear"]').trigger('click')
    await pick(wrapper, '[data-test="pick-files"]', ['1', '2', '3', '4', '5', '6'].map((n) => file(`${n}.txt`)))
    expect(wrapper.find('[data-test="upload-too-many"]').text()).toBe('At most 5 files every 30 min without an account.')
    expect(wrapper.find('[data-test="upload-start"]').attributes('disabled')).toBeDefined()

    await wrapper.find('[data-test="selected-6.txt"] [data-test="selected-remove"]').trigger('click')
    expect(wrapper.find('[data-test="upload-too-many"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="upload-start"]').attributes('disabled')).toBeUndefined()
  })

  it('accepts dropped files', async () => {
    const wrapper = await mountView()
    const card = wrapper.find('[data-test="upload-card"]')
    await card.trigger('dragover')
    expect(card.classes()).toContain('border-gray-400')
    await card.trigger('dragleave')
    expect(card.classes()).not.toContain('border-gray-400')
    await card.trigger('drop', { dataTransfer: { files: [file('d.txt')] } })
    await flushPromises()
    expect(wrapper.find('[data-test="selected-d.txt"]').exists()).toBe(true)

    await card.trigger('drop', { dataTransfer: dataTransfer([dirEntry('/docs', [fileEntry('/docs/e.txt')])]) })
    await flushPromises()
    expect(wrapper.find('[data-test="selected-docs/e.txt"]').exists()).toBe(true)
  })

  it('toasts when a dropped file cannot be read', async () => {
    const wrapper = await mountView()
    await wrapper
      .find('[data-test="upload-card"]')
      .trigger('drop', { dataTransfer: dataTransfer([fileEntry('/x.bin', 1, new Error('unreadable'))]) })
    await flushPromises()
    expect(useToasts().toasts.value.at(-1)).toMatchObject({ type: 'error', message: 'unreadable' })
  })
})
