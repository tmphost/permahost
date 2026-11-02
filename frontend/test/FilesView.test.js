// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'

vi.mock('../src/api', () => ({
  fetchLimits: vi.fn(),
  fetchUsage: vi.fn(),
  listDomains: vi.fn(),
  createDomain: vi.fn(),
  verifyDomain: vi.fn(),
  deleteDomain: vi.fn(),
  listFolders: vi.fn(),
  createFolder: vi.fn(),
  fetchFolder: vi.fn(),
  deleteFolder: vi.fn(),
  uploadFile: vi.fn(),
  deleteFile: vi.fn(),
  renameFolder: vi.fn(),
}))

import {
  createFolder,
  deleteFile,
  deleteFolder,
  fetchFolder,
  fetchLimits,
  fetchUsage,
  listDomains,
  listFolders,
  renameFolder,
  uploadFile,
  verifyDomain,
} from '../src/api'
import FilesView from '../src/views/FilesView.vue'
import { useToasts } from '../src/composables/useToasts'
import { dataTransfer, dirEntry, fileEntry } from './dropEntries'

const NOW = Date.parse('2026-10-18T12:00:00Z')
const pricing = { storageBlockBytes: 100, storageCredits: 500, folderCredits: 300, domainCredits: 500, domainSetupCredits: 500 }
const limits = { maxUploadBytes: 1000, pricing }
const usage = { folders: 2, storageBytes: 50, storageBlocks: 1, domains: 1, monthlyCredits: 1600, pricing }
const folders = [
  { id: 'old', createdAt: '2026-10-18T10:00:00Z', url: 'https://f.test/old/' },
  { id: 'new', createdAt: '2026-10-18T11:00:00Z', sizeBytes: 2060, url: 'https://f.test/new/' },
]
const files = [
  { path: 'site/index.html', size: 2048, updatedAt: '2026-10-18T11:00:00Z', url: 'https://f.test/new/site/index.html' },
  { path: 'a.txt', size: 12, updatedAt: '2026-10-18T11:00:00Z', url: 'https://f.test/new/a.txt' },
]

function file(name, size = 10, webkitRelativePath = '') {
  return { name, size, webkitRelativePath }
}

async function pick(wrapper, selector, list) {
  const input = wrapper.find(selector)
  Object.defineProperty(input.element, 'files', { value: list, configurable: true })
  await input.trigger('change')
}

function deferred() {
  let resolve
  const promise = new Promise((r) => (resolve = r))
  return { promise, resolve }
}

let currentUser
async function mountView({ provide = true } = {}) {
  const wrapper = mount(FilesView, {
    attachTo: document.body,
    global: { provide: provide ? { currentUser } : {} },
  })
  await flushPromises()
  return wrapper
}

const lastToast = () => useToasts().toasts.value.at(-1)

describe('FilesView', () => {
  let writeText
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(NOW)
    useToasts().toasts.value = []
    currentUser = { user: ref({ credits: 10000 }), refreshUser: vi.fn().mockResolvedValue() }
    fetchLimits.mockResolvedValue(limits)
    fetchUsage.mockResolvedValue(usage)
    listDomains.mockResolvedValue([])
    listFolders.mockResolvedValue(folders)
    writeText = vi.fn().mockResolvedValue()
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    Element.prototype.scrollIntoView = vi.fn()
  })
  afterEach(() => {
    vi.useRealTimers()
    useToasts().toasts.value = []
    delete navigator.clipboard
    document.body.innerHTML = ''
  })

  it('shows a spinner, then an empty state', async () => {
    listFolders.mockReturnValueOnce(new Promise(() => {}))
    expect(mount(FilesView).find('[data-test="folders-loading"]').exists()).toBe(true)

    listFolders.mockResolvedValueOnce([])
    const wrapper = await mountView()
    expect(wrapper.find('[data-test="folders-empty"]').text()).toContain('No folders yet')
    expect(wrapper.find('[data-test="upload-summary"]').exists()).toBe(false)
  })

  it('shows what the account hosts and its monthly cost', async () => {
    const wrapper = await mountView()
    expect(wrapper.find('[data-test="usage-folders"]').text()).toBe('2')
    expect(wrapper.find('[data-test="usage-storage"]').text()).toBe('50 B · 1 × 100 B')
    expect(wrapper.find('[data-test="usage-domains"]').text()).toBe('1')
    expect(wrapper.find('[data-test="usage-monthly"]').text()).toBe('1600 credits')
    expect(wrapper.find('[data-test="upload-new-hint"]').text()).toContain('A folder costs 300 credits a month.')
    expect(wrapper.find('[data-test="files-paused"]').exists()).toBe(false)
  })

  it('estimates from an empty account when usage cannot be loaded', async () => {
    fetchUsage.mockRejectedValue(new Error('Failed to load usage (500)'))
    const wrapper = await mountView()
    await pick(wrapper, '[data-test="pick-files"]', [file('a.bin', 100)])
    expect(wrapper.find('[data-test="upload-cost"]').text()).toBe('1 file · 800 credits now, then monthly')
  })

  it('warns that sites are paused when the account is out of credits', async () => {
    currentUser.user.value = { credits: -500 }
    const wrapper = await mountView()
    expect(wrapper.find('[data-test="files-paused"]').text()).toBe(
      'Your sites are paused: your account is 500 credits short. They come back as soon as your credits renew.',
    )
    await pick(wrapper, '[data-test="pick-files"]', [file('a.txt')])
    expect(wrapper.find('[data-test="upload-credits-warning"]').text()).toBe('This upload costs 300 credits but you have -500 left.')
  })

  it('toasts load errors and blocks uploads without limits', async () => {
    fetchLimits.mockRejectedValue(new Error('Failed to load limits (500)'))
    fetchUsage.mockRejectedValue(new Error('Failed to load usage (500)'))
    listFolders.mockRejectedValue(new Error('Failed to load folders (500)'))
    const wrapper = await mountView()
    const messages = useToasts().toasts.value.map((t) => t.message)
    expect(messages).toEqual(['Failed to load limits (500)', 'Failed to load usage (500)', 'Failed to load folders (500)'])
    expect(wrapper.find('[data-test="folders-empty"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="usage"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="upload-new-hint"]').text()).not.toContain('credits')

    await pick(wrapper, '[data-test="pick-files"]', [file('a.txt', 5000)])
    expect(wrapper.find('[data-test="upload-oversized"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="upload-cost"]').text()).toContain('0 credits')
    expect(wrapper.find('[data-test="upload-start"]').attributes('disabled')).toBeDefined()
  })

  it('lists folders newest first with their size', async () => {
    const wrapper = await mountView()
    const ids = wrapper.findAll('[data-test="folder-title"]').map((b) => b.text())
    expect(ids).toEqual(['new', 'old'])
    expect(wrapper.find('[data-test="folder-id"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="folder-new"] [data-test="folder-size"]').text()).toBe('2.0 KB')
    expect(wrapper.find('[data-test="folder-old"] [data-test="folder-size"]').text()).toBe('0 B')
    expect(wrapper.find('[data-test="folder-old"] [data-test="folder-open"]').attributes('href')).toBe('https://f.test/old/')
  })

  it('collects picked files and folders, deduped by path', async () => {
    const wrapper = await mountView()
    await pick(wrapper, '[data-test="pick-files"]', [file('a.txt'), file('b.txt')])
    expect(wrapper.find('[data-test="pick-files"]').element.value).toBe('')
    await pick(wrapper, '[data-test="pick-folder"]', [file('index.html', 10, 'site/index.html'), file('a.txt')])
    expect(wrapper.find('[data-test="pick-folder"]').attributes('webkitdirectory')).toBeDefined()

    const paths = wrapper.findAll('[data-test^="selected-"]:not([data-test="selected-remove"])').map((li) => li.attributes('data-test'))
    expect(paths).toEqual(['selected-a.txt', 'selected-b.txt', 'selected-site/index.html'])
    expect(wrapper.find('[data-test="upload-cost"]').text()).toBe('3 files · 300 credits now, then monthly')

    await wrapper.find('[data-test="selected-b.txt"] [data-test="selected-remove"]').trigger('click')
    await wrapper.find('[data-test="selected-a.txt"] [data-test="selected-remove"]').trigger('click')
    expect(wrapper.find('[data-test="upload-cost"]').text()).toBe('1 file · 300 credits now, then monthly')

    await wrapper.find('[data-test="upload-clear"]').trigger('click')
    expect(wrapper.find('[data-test="upload-summary"]').exists()).toBe(false)
  })

  it('charges a new folder and each storage block the upload starts', async () => {
    const wrapper = await mountView()
    // 50 B stored plus 402 B take 5 blocks of 100 B, 4 more than now.
    await pick(wrapper, '[data-test="pick-files"]', [file('a.bin', 100), file('b.bin', 101), file('c.bin', 201)])
    expect(wrapper.find('[data-test="upload-cost"]').text()).toBe('3 files · 2300 credits now, then monthly')
  })

  it('accepts files dropped anywhere on the page', async () => {
    const wrapper = await mountView()
    const page = wrapper.find('[data-test="files-page"]')
    const overlay = () => wrapper.find('[data-test="drop-overlay"]')
    const files = { types: ['Files'] }

    // Dragging text or a link is not an upload.
    await page.trigger('dragenter', { dataTransfer: { types: ['text/plain'] } })
    expect(overlay().exists()).toBe(false)
    await page.trigger('dragleave', { dataTransfer: { types: ['text/plain'] } })

    // Crossing a child fires enter/leave pairs; the overlay stays up.
    await page.trigger('dragenter', { dataTransfer: files })
    await page.trigger('dragenter', { dataTransfer: files })
    expect(overlay().text()).toContain('Drop files or folders')
    expect(overlay().text()).toContain('They go into a new folder.')
    await page.trigger('dragleave', { dataTransfer: files })
    expect(overlay().exists()).toBe(true)
    await page.trigger('dragleave', { dataTransfer: files })
    expect(overlay().exists()).toBe(false)
    await page.trigger('dragleave', { dataTransfer: files })
    expect(overlay().exists()).toBe(false)

    await page.trigger('dragenter', { dataTransfer: files })
    await page.trigger('dragover', { dataTransfer: files })
    await page.trigger('drop', { dataTransfer: { types: ['Files'], files: [file('dropped.png')] } })
    await flushPromises()
    expect(overlay().exists()).toBe(false)
    expect(wrapper.find('[data-test="selected-dropped.png"]').exists()).toBe(true)
  })

  it('keeps folder paths for dropped folders', async () => {
    const wrapper = await mountView()
    const page = wrapper.find('[data-test="files-page"]')
    const drop = dataTransfer([dirEntry('/site', [fileEntry('/site/index.html'), dirEntry('/site/css', [fileEntry('/site/css/a.css')])])])
    await page.trigger('drop', { dataTransfer: drop })
    await flushPromises()
    expect(wrapper.find('[data-test="selected-site/index.html"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="selected-site/css/a.css"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="upload-cost"]').text()).toContain('2 files')

    uploadFile.mockResolvedValue({})
    createFolder.mockResolvedValue({ folder: { id: 'f1' }, creditsSpent: 300 })
    await wrapper.find('[data-test="upload-start"]').trigger('click')
    await flushPromises()
    expect(uploadFile.mock.calls.map((call) => call[1])).toEqual(['site/index.html', 'site/css/a.css'])
  })

  it('toasts when a dropped file cannot be read', async () => {
    const wrapper = await mountView()
    await wrapper
      .find('[data-test="files-page"]')
      .trigger('drop', { dataTransfer: dataTransfer([fileEntry('/x.bin', 1, new Error('unreadable'))]) })
    await flushPromises()
    expect(lastToast()).toMatchObject({ type: 'error', message: 'unreadable' })
    expect(wrapper.find('[data-test="upload-summary"]').exists()).toBe(false)
  })

  it('flags oversized files and blocks the upload', async () => {
    const wrapper = await mountView()
    await pick(wrapper, '[data-test="pick-files"]', [file('big.bin', 2000), file('ok.txt', 10)])
    expect(wrapper.find('[data-test="upload-oversized"]').text()).toMatch(/^1 file is over the 1000 B limit\. Remove it/)
    expect(wrapper.find('[data-test="selected-big.bin"] .text-rose-600').exists()).toBe(true)
    expect(wrapper.find('[data-test="selected-ok.txt"] .text-rose-600').exists()).toBe(false)
    expect(wrapper.find('[data-test="upload-start"]').attributes('disabled')).toBeDefined()

    await pick(wrapper, '[data-test="pick-files"]', [file('huge.bin', 5000)])
    expect(wrapper.find('[data-test="upload-oversized"]').text()).toMatch(/^2 files are over .* Remove them/)
  })

  it('warns when the upload costs more than the credits left', async () => {
    currentUser.user.value = { credits: 300 }
    const wrapper = await mountView()
    await pick(wrapper, '[data-test="pick-files"]', [file('a.txt'), file('b.txt')])
    expect(wrapper.find('[data-test="upload-credits-warning"]').exists()).toBe(false)
    await pick(wrapper, '[data-test="pick-files"]', [file('c.txt', 100)])
    expect(wrapper.find('[data-test="upload-credits-warning"]').text()).toBe('This upload costs 800 credits but you have 300 left.')
  })

  it('uploads into a new folder with progress, then refreshes', async () => {
    const first = deferred()
    createFolder.mockResolvedValue({ folder: { id: 'f9' }, creditsSpent: 300 })
    uploadFile.mockReturnValueOnce(first.promise).mockResolvedValueOnce({ file: {}, creditsSpent: 1 })
    const wrapper = await mountView()
    await pick(wrapper, '[data-test="pick-folder"]', [file('index.html', 10, 'site/index.html'), file('a b.txt')])
    listFolders.mockResolvedValue([...folders, { id: 'f9', createdAt: '2026-10-18T12:00:00Z', url: 'u' }])

    await wrapper.find('[data-test="upload-start"]').trigger('click')
    await flushPromises()
    expect(createFolder).toHaveBeenCalledWith()
    expect(wrapper.find('[data-test="upload-start"]').text()).toBe('Uploading 0 / 2')
    expect(wrapper.find('[data-test="upload-start"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-test="upload-clear"]').attributes('disabled')).toBeDefined()

    first.resolve({ file: {}, creditsSpent: 1 })
    await flushPromises()
    expect(uploadFile.mock.calls.map((c) => [c[0], c[1]])).toEqual([
      ['f9', 'site/index.html'],
      ['f9', 'a b.txt'],
    ])
    expect(uploadFile.mock.calls[0][2].name).toBe('index.html')
    expect(lastToast()).toMatchObject({ type: 'success', message: 'Uploaded 2 files' })
    expect(wrapper.find('[data-test="upload-summary"]').exists()).toBe(false)
    expect(currentUser.refreshUser).toHaveBeenCalledTimes(1)
    expect(fetchUsage).toHaveBeenCalledTimes(2)
    expect(wrapper.findAll('[data-test="folder-title"]').map((b) => b.text())).toEqual(['f9', 'new', 'old'])
  })

  it('says 1 file for a single upload', async () => {
    createFolder.mockResolvedValue({ folder: { id: 'f1' }, creditsSpent: 300 })
    uploadFile.mockResolvedValue({ file: {}, creditsSpent: 1 })
    const wrapper = await mountView()
    await pick(wrapper, '[data-test="pick-files"]', [file('a.txt')])
    await wrapper.find('[data-test="upload-start"]').trigger('click')
    await flushPromises()
    expect(lastToast().message).toBe('Uploaded 1 file')
  })

  it('reports a failed upload and still refreshes', async () => {
    createFolder.mockResolvedValue({ folder: { id: 'f1' }, creditsSpent: 300 })
    uploadFile.mockRejectedValue(Object.assign(new Error('not enough credits'), { status: 402 }))
    const wrapper = await mountView()
    await pick(wrapper, '[data-test="pick-files"]', [file('a.txt')])
    await wrapper.find('[data-test="upload-start"]').trigger('click')
    await flushPromises()
    expect(lastToast()).toMatchObject({ type: 'error', message: 'not enough credits' })
    expect(wrapper.find('[data-test="upload-summary"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="upload-start"]').text()).toBe('Upload')
    expect(currentUser.refreshUser).toHaveBeenCalled()
    expect(listFolders).toHaveBeenCalledTimes(2)
  })

  it('works without a provided user', async () => {
    createFolder.mockResolvedValue({ folder: { id: 'f1' }, creditsSpent: 300 })
    uploadFile.mockResolvedValue({ file: {}, creditsSpent: 1 })
    const wrapper = await mountView({ provide: false })
    await pick(wrapper, '[data-test="pick-files"]', [file('a.txt')])
    expect(wrapper.find('[data-test="upload-credits-warning"]').exists()).toBe(false)
    await wrapper.find('[data-test="upload-start"]').trigger('click')
    await flushPromises()
    expect(lastToast().type).toBe('success')
  })

  it('uploads into an existing folder, marking the files it replaces', async () => {
    fetchFolder.mockResolvedValue({ folder: folders[1], files })
    uploadFile.mockResolvedValue({ file: {}, creditsSpent: 1 })
    const wrapper = await mountView()
    await wrapper.find('[data-test="folder-new"] [data-test="folder-toggle"]').trigger('click')
    await flushPromises()
    await pick(wrapper, '[data-test="pick-files"]', [file('a.txt'), file('b.txt')])
    expect(wrapper.find('[data-test="selected-replaces"]').exists()).toBe(false)

    await wrapper.find('[data-test="folder-new"] [data-test="folder-upload"]').trigger('click')
    await flushPromises()
    expect(Element.prototype.scrollIntoView).toHaveBeenCalled()
    expect(wrapper.find('[data-test="upload-target"]').text()).toContain('Upload to new')
    expect(wrapper.find('[data-test="upload-target-hint"]').text()).toContain('No folder charge')
    expect(wrapper.find('[data-test="upload-new-hint"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="folder-new"]').classes()).toContain('border-gray-900')
    expect(wrapper.find('[data-test="folder-old"]').classes()).not.toContain('border-gray-900')
    expect(wrapper.find('[data-test="selected-a.txt"] [data-test="selected-replaces"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="selected-b.txt"] [data-test="selected-replaces"]').exists()).toBe(false)
    // a.txt shrinks from 12 B to 10 B and b.txt adds 10 B: 58 B fit the paid block.
    expect(wrapper.find('[data-test="upload-cost"]').text()).toBe('2 files · 1 replaced · 0 credits')

    await wrapper.find('[data-test="files-page"]').trigger('dragenter', { dataTransfer: { types: ['Files'] } })
    expect(wrapper.find('[data-test="drop-overlay"]').text()).toContain('They go into new, replacing files with the same path.')
    await wrapper.find('[data-test="files-page"]').trigger('dragleave', { dataTransfer: { types: ['Files'] } })

    await wrapper.find('[data-test="upload-start"]').trigger('click')
    await flushPromises()
    expect(createFolder).not.toHaveBeenCalled()
    expect(uploadFile.mock.calls.map((c) => [c[0], c[1]])).toEqual([
      ['new', 'a.txt'],
      ['new', 'b.txt'],
    ])
    expect(lastToast()).toMatchObject({ type: 'success', message: 'Uploaded 2 files' })
    // The expanded listing is reloaded; the card is back to a new folder.
    expect(fetchFolder).toHaveBeenCalledTimes(3)
    expect(wrapper.find('[data-test="folder-new"] [data-test="folder-files"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="upload-target"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="upload-new-hint"]').exists()).toBe(true)
  })

  it('targets the last picked folder and switches back to a new one', async () => {
    const slow = deferred()
    fetchFolder.mockReturnValueOnce(slow.promise).mockResolvedValueOnce({ folder: folders[1], files })
    const wrapper = await mountView()
    await pick(wrapper, '[data-test="pick-files"]', [file('a.txt'), file('b.txt')])
    await wrapper.find('[data-test="folder-old"] [data-test="folder-upload"]').trigger('click')
    await wrapper.find('[data-test="folder-new"] [data-test="folder-upload"]').trigger('click')
    await flushPromises()
    slow.resolve({ folder: folders[0], files: [{ path: 'b.txt', size: 1 }] })
    await flushPromises()
    expect(wrapper.find('[data-test="upload-target"]').text()).toContain('new')
    expect(wrapper.find('[data-test="selected-a.txt"] [data-test="selected-replaces"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="selected-b.txt"] [data-test="selected-replaces"]').exists()).toBe(false)

    // The card follows a rename of its folder.
    renameFolder.mockResolvedValueOnce({ ...folders[1], title: 'Photos', defaultTitle: 'new' })
    await wrapper.find('[data-test="folder-new"] [data-test="folder-title"]').trigger('click')
    await wrapper.find('[data-test="folder-title-input"]').setValue('Photos')
    await wrapper.find('[data-test="folder-title-input"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(wrapper.find('[data-test="upload-target"]').text()).toContain('Upload to Photos')

    await wrapper.find('[data-test="upload-new-folder"]').trigger('click')
    expect(wrapper.find('[data-test="upload-target"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="selected-replaces"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="upload-cost"]').text()).toBe('2 files · 300 credits now, then monthly')

    // Without the folder's listing, nothing is marked but the upload still works.
    fetchFolder.mockRejectedValueOnce(new Error('Failed to load folder (500)'))
    await wrapper.find('[data-test="folder-old"] [data-test="folder-upload"]').trigger('click')
    await flushPromises()
    expect(lastToast()).toMatchObject({ type: 'error', message: 'Failed to load folder (500)' })
    expect(wrapper.find('[data-test="upload-target"]').text()).toContain('old')
    expect(wrapper.find('[data-test="upload-start"]').attributes('disabled')).toBeUndefined()
  })

  it('reports a folder that cannot be created', async () => {
    createFolder.mockRejectedValue(new Error('Failed to create folder (500)'))
    const wrapper = await mountView()
    await pick(wrapper, '[data-test="pick-files"]', [file('a.txt')])
    await wrapper.find('[data-test="upload-start"]').trigger('click')
    await flushPromises()
    expect(lastToast()).toMatchObject({ type: 'error', message: 'Failed to create folder (500)' })
    expect(uploadFile).not.toHaveBeenCalled()
    expect(listFolders).toHaveBeenCalledTimes(2)
  })

  it('keeps the upload target in step with deletions', async () => {
    fetchFolder.mockResolvedValue({ folder: folders[1], files })
    deleteFile.mockResolvedValue()
    deleteFolder.mockResolvedValue()
    const wrapper = await mountView()
    await pick(wrapper, '[data-test="pick-files"]', [file('a.txt')])
    await wrapper.find('[data-test="folder-new"] [data-test="folder-toggle"]').trigger('click')
    await wrapper.find('[data-test="folder-new"] [data-test="folder-upload"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="selected-replaces"]').exists()).toBe(true)

    await wrapper.find('[data-test="file-a.txt"] [data-test="file-delete"]').trigger('click')
    await wrapper.find('[data-test="delete-modal-confirm"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="selected-replaces"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="upload-target"]').exists()).toBe(true)

    // Deleting another folder keeps the target.
    await wrapper.find('[data-test="folder-old"] [data-test="folder-delete"]').trigger('click')
    await wrapper.find('[data-test="delete-modal-confirm"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="upload-target"]').exists()).toBe(true)

    await wrapper.find('[data-test="folder-new"] [data-test="folder-delete"]').trigger('click')
    await wrapper.find('[data-test="delete-modal-confirm"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="upload-target"]').exists()).toBe(false)
  })

  it('expands a folder to its files and collapses it', async () => {
    const pending = deferred()
    fetchFolder.mockReturnValueOnce(pending.promise)
    const wrapper = await mountView()
    const toggle = wrapper.find('[data-test="folder-new"] [data-test="folder-toggle"]')
    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('true')
    expect(toggle.attributes('aria-label')).toBe('Hide files')
    expect(wrapper.find('[data-test="folder-files"]').text()).toContain('Loading files')

    pending.resolve({ folder: folders[1], files })
    await flushPromises()
    expect(fetchFolder).toHaveBeenCalledWith('new')
    const row = wrapper.find('[data-test="file-site/index.html"]')
    expect(row.text()).toContain('2.0 KB')
    const link = row.find('[data-test="file-link"]')
    expect(link.text()).toBe('site/index.html')
    expect(link.attributes()).toMatchObject({ href: files[0].url, target: '_blank', rel: 'noopener' })
    expect(wrapper.find('[data-test="file-a.txt"]').text()).toContain('12 B')

    await toggle.trigger('click')
    expect(wrapper.find('[data-test="folder-files"]').exists()).toBe(false)
    expect(toggle.attributes('aria-expanded')).toBe('false')
    expect(toggle.attributes('aria-label')).toBe('Show files')
  })

  it('names folders by title, else their default title', async () => {
    listFolders.mockResolvedValue([
      { ...folders[0], title: 'Holiday', defaultTitle: 'old' },
      { ...folders[1], title: '', defaultTitle: 'report.pdf' },
    ])
    const wrapper = await mountView()
    expect(wrapper.findAll('[data-test="folder-title"]').map((b) => b.text())).toEqual(['report.pdf', 'Holiday'])
    expect(wrapper.find('[data-test="folder-new"] [data-test="folder-id"]').text()).toBe('new')
    expect(wrapper.find('[data-test="folder-old"] [data-test="folder-id"]').text()).toBe('old')
  })

  it('renames a folder inline', async () => {
    renameFolder.mockResolvedValueOnce({ ...folders[1], title: 'Photos', defaultTitle: 'new' })
    const wrapper = await mountView()
    const card = () => wrapper.find('[data-test="folder-new"]')
    await card().find('[data-test="folder-title"]').trigger('click')
    await flushPromises()
    const input = card().find('[data-test="folder-title-input"]')
    expect(input.element.value).toBe('new')
    expect(input.attributes('placeholder')).toBe('new')
    expect(document.activeElement).toBe(input.element)
    expect(card().find('[data-test="folder-title"]').exists()).toBe(false)

    await input.setValue('  Photos ')
    await input.trigger('keydown', { key: 'Enter' })
    await input.trigger('blur')
    await flushPromises()
    expect(renameFolder).toHaveBeenCalledTimes(1)
    expect(renameFolder).toHaveBeenCalledWith('new', 'Photos')
    expect(card().find('[data-test="folder-title"]').text()).toBe('Photos')
    expect(card().find('[data-test="folder-id"]').text()).toBe('new')
    expect(lastToast()).toMatchObject({ type: 'success', message: 'Folder renamed' })
  })

  it('clears a title to restore the default one', async () => {
    listFolders.mockResolvedValue([{ ...folders[1], title: 'Photos', defaultTitle: 'a.txt' }])
    renameFolder.mockResolvedValueOnce({ ...folders[1], title: '', defaultTitle: 'a.txt' })
    const wrapper = await mountView()
    await wrapper.find('[data-test="folder-title"]').trigger('click')
    const input = wrapper.find('[data-test="folder-title-input"]')
    expect(input.attributes('placeholder')).toBe('a.txt')
    await input.setValue('')
    await input.trigger('blur')
    await flushPromises()
    expect(renameFolder).toHaveBeenCalledWith('new', '')
    expect(wrapper.find('[data-test="folder-title"]').text()).toBe('a.txt')
  })

  it('leaves the title alone when unchanged or cancelled', async () => {
    const wrapper = await mountView()
    const card = () => wrapper.find('[data-test="folder-old"]')
    await card().find('[data-test="folder-title"]').trigger('click')
    await card().find('[data-test="folder-title-input"]').trigger('keydown', { key: 'Enter' })
    expect(card().find('[data-test="folder-title-input"]').exists()).toBe(false)

    await card().find('[data-test="folder-title"]').trigger('click')
    const input = card().find('[data-test="folder-title-input"]')
    await input.setValue('Something')
    await input.trigger('keydown', { key: 'Escape' })
    expect(card().find('[data-test="folder-title"]').text()).toBe('old')
    expect(renameFolder).not.toHaveBeenCalled()
  })

  it('toasts rename errors and keeps the old title', async () => {
    renameFolder.mockRejectedValueOnce(new Error('invalid folder title'))
    const wrapper = await mountView()
    await wrapper.find('[data-test="folder-old"] [data-test="folder-title"]').trigger('click')
    const input = wrapper.find('[data-test="folder-title-input"]')
    await input.setValue('x')
    await input.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(lastToast()).toMatchObject({ type: 'error', message: 'invalid folder title' })
    expect(wrapper.find('[data-test="folder-old"] [data-test="folder-title"]').text()).toBe('old')
  })

  it('shows an empty folder and toasts load errors', async () => {
    fetchFolder.mockResolvedValueOnce({ folder: folders[0], files: [] })
    fetchFolder.mockRejectedValueOnce(new Error('folder not found'))
    const wrapper = await mountView()
    await wrapper.find('[data-test="folder-old"] [data-test="folder-toggle"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="folder-old"] [data-test="folder-files"]').text()).toContain('This folder is empty.')

    await wrapper.find('[data-test="folder-new"] [data-test="folder-toggle"]').trigger('click')
    await flushPromises()
    expect(lastToast()).toMatchObject({ type: 'error', message: 'folder not found' })
    expect(wrapper.find('[data-test="folder-new"] [data-test="folder-files"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="folder-old"] [data-test="folder-files"]').exists()).toBe(true)
  })

  it('copies folder and file URLs', async () => {
    fetchFolder.mockResolvedValue({ folder: folders[1], files })
    const wrapper = await mountView()
    await wrapper.find('[data-test="folder-new"] [data-test="folder-copy"]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith('https://f.test/new/')
    expect(lastToast()).toMatchObject({ type: 'success', message: 'Folder URL copied' })

    await wrapper.find('[data-test="folder-new"] [data-test="folder-toggle"]').trigger('click')
    await flushPromises()
    writeText.mockRejectedValueOnce(new Error('denied'))
    await wrapper.find('[data-test="file-a.txt"] [data-test="file-copy"]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenLastCalledWith('https://f.test/new/a.txt')
    expect(lastToast().type).toBe('error')
    await wrapper.find('[data-test="file-a.txt"] [data-test="file-copy"]').trigger('click')
    await flushPromises()
    expect(lastToast()).toMatchObject({ type: 'success', message: 'File URL copied' })
  })

  it('deletes a folder after confirmation', async () => {
    deleteFolder.mockResolvedValue()
    const wrapper = await mountView()
    await wrapper.find('[data-test="folder-old"] [data-test="folder-delete"]').trigger('click')
    const modal = wrapper.find('[data-test="delete-modal"]')
    expect(modal.text()).toContain('Delete folder?')
    expect(modal.text()).toContain('Folder old, its files and its custom domains are removed.')

    await wrapper.find('[data-test="delete-modal-cancel"]').trigger('click')
    expect(wrapper.find('[data-test="delete-modal"]').exists()).toBe(false)

    await wrapper.find('[data-test="folder-old"] [data-test="folder-delete"]').trigger('click')
    await wrapper.find('[data-test="delete-modal-confirm"]').trigger('click')
    await flushPromises()
    expect(deleteFolder).toHaveBeenCalledWith('old')
    expect(wrapper.find('[data-test="folder-old"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="delete-modal"]').exists()).toBe(false)
    expect(lastToast()).toMatchObject({ type: 'success', message: 'Folder deleted' })
    expect(fetchUsage).toHaveBeenCalledTimes(2)
  })

  it('deletes a file after confirmation', async () => {
    fetchFolder.mockResolvedValue({ folder: folders[1], files })
    deleteFile.mockResolvedValue()
    const wrapper = await mountView()
    await wrapper.find('[data-test="folder-new"] [data-test="folder-toggle"]').trigger('click')
    await flushPromises()
    await wrapper.find('[data-test="file-a.txt"] [data-test="file-delete"]').trigger('click')
    const modal = wrapper.find('[data-test="delete-modal"]')
    expect(modal.text()).toContain('Delete file?')
    expect(modal.text()).toContain('a.txt is removed from folder new')

    const pending = deferred()
    deleteFile.mockReturnValueOnce(pending.promise)
    await wrapper.find('[data-test="delete-modal-confirm"]').trigger('click')
    expect(wrapper.find('[data-test="delete-modal-confirm"]').text()).toBe('Deleting…')
    pending.resolve()
    await flushPromises()
    expect(deleteFile).toHaveBeenCalledWith('new', 'a.txt')
    expect(listFolders).toHaveBeenCalledTimes(2)
    expect(fetchUsage).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[data-test="file-a.txt"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="file-site/index.html"]').exists()).toBe(true)
    expect(lastToast()).toMatchObject({ type: 'success', message: 'File deleted' })
  })

  it('toasts delete errors and keeps the folder', async () => {
    deleteFolder.mockRejectedValue(new Error('Failed to delete folder (500)'))
    const wrapper = await mountView()
    await wrapper.find('[data-test="folder-new"] [data-test="folder-delete"]').trigger('click')
    await wrapper.find('[data-test="delete-modal-confirm"]').trigger('click')
    await flushPromises()
    expect(lastToast()).toMatchObject({ type: 'error', message: 'Failed to delete folder (500)' })
    expect(wrapper.find('[data-test="folder-new"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="delete-modal"]').exists()).toBe(false)
  })
  it('opens the custom domains of a folder and refreshes credits when they change', async () => {
    const domain = { id: 'd1', folderId: 'new', hostname: 'www.example.com', target: 'fnew.f.test', status: 'pending' }
    listDomains.mockResolvedValue([domain])
    verifyDomain.mockResolvedValue({ ...domain, status: 'active' })
    const wrapper = await mountView()
    const toggle = wrapper.find('[data-test="folder-new"] [data-test="folder-domains-toggle"]')
    expect(toggle.attributes('aria-pressed')).toBe('false')
    await toggle.trigger('click')
    await flushPromises()
    expect(toggle.attributes('aria-pressed')).toBe('true')
    expect(listDomains).toHaveBeenCalledWith('new')
    expect(wrapper.find('[data-test="folder-new"] [data-test="domain-pricing"]').text()).toContain(
      '500 credits to set up and 500 a month',
    )
    expect(wrapper.find('[data-test="folder-old"] [data-test="folder-domains"]').exists()).toBe(false)

    await wrapper.find('[data-test="domain-verify"]').trigger('click')
    await flushPromises()
    expect(currentUser.refreshUser).toHaveBeenCalledTimes(1)
    expect(fetchUsage).toHaveBeenCalledTimes(2)

    await toggle.trigger('click')
    expect(wrapper.find('[data-test="folder-domains"]').exists()).toBe(false)
  })
})
