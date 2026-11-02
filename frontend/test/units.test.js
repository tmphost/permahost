// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { usePlatformStore } from '../src/stores/platformStore'
import { useToasts } from '../src/composables/useToasts'
import { creditsPercent, renewsIn } from '../src/utils/credits'
import { loginErrorMessage } from '../src/utils/loginErrors'
import {
  droppedFiles,
  fileItems,
  formatBytes,
  formatMinutes,
  isFileDrag,
  relativePath,
  storageBlocks,
  uploadCost,
} from '../src/utils/files'
import { dataTransfer, dirEntry, fileEntry } from './dropEntries'
import { useClipboard } from '../src/composables/useClipboard'

describe('platformStore', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('accepts known platforms only', () => {
    const store = usePlatformStore()
    expect(store.isWeb).toBe(true)
    store.setPlatform('desktop')
    expect(store.isDesktop).toBe(true)
    expect(store.isWeb).toBe(false)
    store.setPlatform('toaster')
    expect(store.platform).toBe('web')
  })
})

describe('useToasts', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => {
    useToasts().toasts.value = []
    vi.useRealTimers()
  })

  it('adds, expires and caps toasts', () => {
    const { toasts, notifyInfo, notifySuccess, notifyError, removeToast } = useToasts()
    const id = notifyInfo('hello', 'Title')
    expect(toasts.value[0]).toMatchObject({ id, message: 'hello', type: 'info', title: 'Title' })
    notifySuccess('ok')
    notifyError('bad')
    expect(toasts.value.map((t) => t.type)).toEqual(['info', 'success', 'error'])

    vi.advanceTimersByTime(4000)
    expect(toasts.value.map((t) => t.type)).toEqual(['error'])
    vi.advanceTimersByTime(4000)
    expect(toasts.value).toEqual([])

    for (let i = 0; i < 6; i++) notifyInfo(`m${i}`)
    expect(toasts.value.map((t) => t.message)).toEqual(['m2', 'm3', 'm4', 'm5'])
    removeToast(toasts.value[0].id)
    expect(toasts.value).toHaveLength(3)
  })

  it('addToast defaults to info', () => {
    const { toasts, addToast } = useToasts()
    addToast('plain')
    expect(toasts.value[0]).toMatchObject({ type: 'info', title: null })
  })
})

describe('credits', () => {
  it('creditsPercent clamps and handles missing data', () => {
    expect(creditsPercent(null)).toBe(0)
    expect(creditsPercent({ credits: 5, creditsLimit: 0 })).toBe(0)
    expect(creditsPercent({ credits: 125, creditsLimit: 250 })).toBe(50)
    expect(creditsPercent({ credits: 300, creditsLimit: 250 })).toBe(100)
    expect(creditsPercent({ credits: -1, creditsLimit: 250 })).toBe(0)
  })

  it('renewsIn describes the next reset', () => {
    const now = Date.parse('2026-10-18T12:00:00Z')
    expect(renewsIn('garbage', now)).toBe('')
    expect(renewsIn(undefined, now)).toBe('')
    expect(renewsIn('2026-10-18T00:00:00Z', now)).toBe('Renews today')
    expect(renewsIn('2026-10-19T00:00:00Z', now)).toBe('Renews tomorrow')
    expect(renewsIn('2026-11-17T00:00:00Z', now)).toBe('Renews in 30 days')
    expect(renewsIn(new Date(Date.now() + 36 * 3600 * 1000).toISOString())).toBe('Renews in 2 days')
  })
})

describe('loginErrorMessage', () => {
  it('maps known codes and falls back', () => {
    expect(loginErrorMessage(undefined)).toBe('')
    expect(loginErrorMessage('cancelled')).toBe('Sign-in was cancelled.')
    expect(loginErrorMessage('email_not_verified')).toMatch(/verified email/)
    expect(loginErrorMessage('weird')).toMatch(/Something went wrong/)
  })
})

describe('providers', () => {
  it('slugs names like the server', async () => {
    const { providerSlug } = await import('../src/utils/providers')
    expect(providerSlug(' My  Okta! ')).toBe('my-okta')
    expect(providerSlug('--A_b--')).toBe('a-b')
    expect(providerSlug('')).toBe('')
    expect(providerSlug(undefined)).toBe('')
  })

  it('renders SVG as a data URI', async () => {
    const { svgDataUri } = await import('../src/utils/providers')
    expect(svgDataUri('<svg a="#"/>')).toBe('data:image/svg+xml;charset=utf-8,%3Csvg%20a%3D%22%23%22%2F%3E')
  })
})

describe('files utils', () => {
  it('formatMinutes picks a readable unit', () => {
    expect(formatMinutes(30)).toBe('30 min')
    expect(formatMinutes(60)).toBe('1 h')
    expect(formatMinutes(90)).toBe('1 h 30 min')
    expect(formatMinutes(720)).toBe('12 h')
    expect(formatMinutes(1440)).toBe('1 day')
    expect(formatMinutes(10080)).toBe('7 days')
    expect(formatMinutes(432000)).toBe('300 days')
    expect(formatMinutes(1500)).toBe('25 h')
  })

  it('storageBlocks counts started blocks', () => {
    expect(storageBlocks(0, 100)).toBe(0)
    expect(storageBlocks(-5, 100)).toBe(0)
    expect(storageBlocks(1, 100)).toBe(1)
    expect(storageBlocks(100, 100)).toBe(1)
    expect(storageBlocks(101, 100)).toBe(2)
  })

  it('uploadCost charges a new folder and each storage block started', () => {
    const pricing = { storageBlockBytes: 100, storageCredits: 500, folderCredits: 300 }
    expect(uploadCost({ newFolder: true, storedBytes: 0, addedBytes: 0, pricing })).toBe(300)
    expect(uploadCost({ newFolder: true, storedBytes: 0, addedBytes: 1, pricing })).toBe(800)
    expect(uploadCost({ newFolder: false, storedBytes: 50, addedBytes: 50, pricing })).toBe(0)
    expect(uploadCost({ newFolder: false, storedBytes: 50, addedBytes: 151, pricing })).toBe(1000)
    expect(uploadCost({ newFolder: false, storedBytes: 250, addedBytes: -200, pricing })).toBe(0)
  })

  it('formatBytes scales units', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(1023)).toBe('1023 B')
    expect(formatBytes(1536)).toBe('1.5 KB')
    expect(formatBytes(16 * 1024 * 1024)).toBe('16.0 MB')
    expect(formatBytes(3 * 1024 ** 3)).toBe('3.0 GB')
    expect(formatBytes(2048 * 1024 ** 3)).toBe('2048.0 GB')
  })

  it('relativePath prefers the folder-relative path', () => {
    expect(relativePath({ name: 'a.txt', webkitRelativePath: 'dir/a.txt' })).toBe('dir/a.txt')
    expect(relativePath({ name: 'a.txt', webkitRelativePath: '' })).toBe('a.txt')
    expect(relativePath({ name: 'b.txt' })).toBe('b.txt')
  })

  it('fileItems pairs files with their paths', () => {
    const a = { name: 'a.txt', webkitRelativePath: 'dir/a.txt' }
    const b = { name: 'b.txt', webkitRelativePath: '' }
    expect(fileItems([a, b])).toEqual([
      { path: 'dir/a.txt', file: a },
      { path: 'b.txt', file: b },
    ])
  })

  it('isFileDrag only accepts drags that carry files', () => {
    expect(isFileDrag({ dataTransfer: { types: ['Files'] } })).toBe(true)
    expect(isFileDrag({ dataTransfer: { types: ['text/plain'] } })).toBe(false)
    expect(isFileDrag({ dataTransfer: {} })).toBe(false)
    expect(isFileDrag({})).toBe(false)
  })

  it('droppedFiles walks dropped folders', async () => {
    const tree = dataTransfer([
      fileEntry('/top.txt'),
      dirEntry(
        '/site',
        [fileEntry('/site/index.html'), dirEntry('/site/css', [fileEntry('/site/css/a.css'), fileEntry('/site/css/b.css')], 1)],
        1,
      ),
      dirEntry('/empty', []),
    ])
    const items = await droppedFiles(tree)
    expect(items.map((item) => item.path)).toEqual(['top.txt', 'site/index.html', 'site/css/a.css', 'site/css/b.css'])
    expect(items[1].file).toMatchObject({ name: 'index.html', size: 10 })
  })

  it('droppedFiles skips non-file items and falls back to the file list', async () => {
    const file = { name: 'a.txt', webkitRelativePath: '' }
    expect(await droppedFiles({ files: [file] })).toEqual([{ path: 'a.txt', file }])
    const noEntries = { files: [file], items: [{ kind: 'string', webkitGetAsEntry: () => null }, { kind: 'file' }] }
    expect(await droppedFiles(noEntries)).toEqual([{ path: 'a.txt', file }])
  })

  it('droppedFiles rejects when a file cannot be read', async () => {
    await expect(droppedFiles(dataTransfer([fileEntry('/x.bin', 1, new Error('unreadable'))]))).rejects.toThrow('unreadable')
  })
})

describe('useClipboard', () => {
  afterEach(() => {
    useToasts().toasts.value = []
  })

  it('copies and toasts success or failure', async () => {
    const writeText = vi.fn().mockResolvedValueOnce().mockRejectedValueOnce(new Error('denied'))
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    const { copy } = useClipboard()
    expect(await copy('hello')).toBe(true)
    expect(writeText).toHaveBeenCalledWith('hello')
    expect(useToasts().toasts.value.at(-1)).toMatchObject({ type: 'success', message: 'Copied to clipboard' })
    expect(await copy('x', 'Link copied')).toBe(false)
    expect(useToasts().toasts.value.at(-1)).toMatchObject({ type: 'error', message: 'Could not copy to the clipboard' })
    delete navigator.clipboard
  })
})
