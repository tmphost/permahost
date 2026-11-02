// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

function json(body, status = 200) {
  return { ok: status >= 200 && status < 300, status, json: async () => body }
}

async function loadApi(basePath) {
  vi.resetModules()
  window.__TMPHOST_BASE_PATH__ = basePath
  return import('../src/api.js')
}

describe('api', () => {
  beforeEach(() => {
    globalThis.fetch = vi.fn()
  })
  afterEach(() => {
    delete window.__TMPHOST_BASE_PATH__
  })

  it('builds the API base from the injected base path', async () => {
    expect((await loadApi('/app/')).API_BASE_URL).toBe('/app/api/v1')
    expect((await loadApi(undefined)).API_BASE_URL).toBe('/api/v1')
  })

  it('fetchUser caches the user and dedupes concurrent calls', async () => {
    const api = await loadApi('')
    fetch.mockResolvedValue(json({ user: { id: 'u1' } }))
    const [a, b] = await Promise.all([api.fetchUser(), api.fetchUser()])
    expect(a).toEqual({ id: 'u1' })
    expect(b).toBe(a)
    expect(await api.fetchUser()).toBe(a)
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(fetch).toHaveBeenCalledWith('/api/v1/users/me', { credentials: 'same-origin' })

    api.clearUserCache()
    await api.fetchUser()
    expect(fetch).toHaveBeenCalledTimes(2)
  })

  it('fetchUser returns null without a session', async () => {
    const api = await loadApi('')
    fetch.mockResolvedValue(json({ error: 'unauthorized' }, 401))
    expect(await api.fetchUser()).toBeNull()
  })

  it('fetchUser throws on server errors and can retry', async () => {
    const api = await loadApi('')
    fetch.mockResolvedValueOnce(json({}, 500))
    await expect(api.fetchUser()).rejects.toThrow('Failed to load user (500)')
    fetch.mockResolvedValueOnce(json({ user: { id: 'u2' } }))
    expect(await api.fetchUser()).toEqual({ id: 'u2' })
  })

  it('fetchAuthConfig returns providers', async () => {
    const api = await loadApi('')
    fetch.mockResolvedValue(json({ providers: ['google'] }))
    expect(await api.fetchAuthConfig()).toEqual(['google'])
    fetch.mockResolvedValue(json({}, 503))
    await expect(api.fetchAuthConfig()).rejects.toThrow('Failed to load sign-in options (503)')
  })

  it('providerLoginUrl encodes the redirect', async () => {
    const api = await loadApi('')
    expect(api.providerLoginUrl('github')).toBe('/auth/github/login')
    expect(api.providerLoginUrl('google', '/a?b=1')).toBe('/auth/google/login?redirect_url=%2Fa%3Fb%3D1')
  })

  it('logout posts and clears the cache even on failure', async () => {
    const api = await loadApi('')
    fetch.mockResolvedValue(json({ user: { id: 'u1' } }))
    await api.fetchUser()

    fetch.mockResolvedValueOnce(json(null, 204))
    await api.logout()
    expect(fetch).toHaveBeenLastCalledWith('/auth/logout', { method: 'POST', credentials: 'same-origin' })

    fetch.mockResolvedValueOnce(json({ user: { id: 'u1' } }))
    await api.fetchUser()
    fetch.mockRejectedValueOnce(new Error('offline'))
    await expect(api.logout()).rejects.toThrow('offline')
    fetch.mockResolvedValueOnce(json({}, 401))
    expect(await api.fetchUser()).toBeNull()
  })
})

describe('setup api', () => {
  beforeEach(() => {
    globalThis.fetch = vi.fn()
  })

  it('setupLogin posts the token and surfaces API errors', async () => {
    const api = await loadApi('')
    fetch.mockResolvedValueOnce(json(null, 204))
    await api.setupLogin('tok')
    expect(fetch).toHaveBeenCalledWith('/api/v1/setup/login', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: '{"token":"tok"}',
    })

    fetch.mockResolvedValueOnce(json({ error: 'invalid root access token' }, 401))
    await expect(api.setupLogin('x')).rejects.toMatchObject({ message: 'invalid root access token', status: 401 })

    fetch.mockResolvedValueOnce({ ok: false, status: 502, json: async () => { throw new Error('html') } })
    await expect(api.setupLogin('x')).rejects.toThrow('Sign-in failed (502)')

    fetch.mockResolvedValueOnce(json({}, 500))
    await expect(api.setupLogin('x')).rejects.toThrow('Sign-in failed (500)')
  })

  it('setupLogout posts', async () => {
    const api = await loadApi('')
    fetch.mockResolvedValueOnce(json(null, 204))
    await api.setupLogout()
    expect(fetch).toHaveBeenCalledWith('/api/v1/setup/logout', { method: 'POST', credentials: 'same-origin' })
  })

  it('fetchSettings returns state, null without a session, throws otherwise', async () => {
    const api = await loadApi('')
    fetch.mockResolvedValueOnce(json({ settings: {}, secretsSet: {}, restartRequired: false }))
    expect(await api.fetchSettings()).toEqual({ settings: {}, secretsSet: {}, restartRequired: false })
    fetch.mockResolvedValueOnce(json({ error: 'unauthorized' }, 401))
    expect(await api.fetchSettings()).toBeNull()
    fetch.mockResolvedValueOnce(json({}, 500))
    await expect(api.fetchSettings()).rejects.toThrow('Failed to load settings (500)')
  })

  it('saveSettings puts and surfaces validation errors', async () => {
    const api = await loadApi('')
    fetch.mockResolvedValueOnce(json({ settings: { credits: { monthlyGrant: 9 } } }))
    expect(await api.saveSettings({ credits: { monthlyGrant: 9 } })).toEqual({ settings: { credits: { monthlyGrant: 9 } } })
    expect(fetch).toHaveBeenCalledWith('/api/v1/setup/settings', {
      method: 'PUT',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: '{"settings":{"credits":{"monthlyGrant":9}}}',
    })
    fetch.mockResolvedValueOnce(json({ error: 'invalid settings: sqlite DSN is required' }, 400))
    await expect(api.saveSettings({})).rejects.toMatchObject({ message: 'invalid settings: sqlite DSN is required', status: 400 })
  })
})

describe('files api', () => {
  beforeEach(() => {
    globalThis.fetch = vi.fn()
  })

  it('exports the base path', async () => {
    expect((await loadApi('/app/')).BASE_PATH).toBe('/app')
  })

  it('filePath encodes each segment', async () => {
    const api = await loadApi('')
    expect(api.filePath('a b/c#d/e.txt')).toBe('a%20b/c%23d/e.txt')
  })

  it('fetchLimits returns the limits', async () => {
    const api = await loadApi('')
    const limits = { defaultTtlMinutes: 30, maxTtlMinutes: 60, maxUploadBytes: 10, billingBlockMinutes: 30 }
    fetch.mockResolvedValueOnce(json(limits))
    expect(await api.fetchLimits()).toEqual(limits)
    expect(fetch).toHaveBeenCalledWith('/api/v1/limits', { method: 'GET', credentials: 'same-origin' })
    fetch.mockResolvedValueOnce(json({}, 500))
    await expect(api.fetchLimits()).rejects.toMatchObject({ message: 'Failed to load limits (500)', status: 500 })
  })

  it('lists, creates, fetches and deletes folders', async () => {
    const api = await loadApi('')
    fetch.mockResolvedValueOnce(json({ folders: [{ id: 'f1' }] }))
    expect(await api.listFolders()).toEqual([{ id: 'f1' }])

    fetch.mockResolvedValueOnce(json({ folder: { id: 'f2' } }, 201))
    expect(await api.createFolder(60)).toEqual({ id: 'f2' })
    expect(fetch).toHaveBeenLastCalledWith('/api/v1/folders', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: '{"ttlMinutes":60}',
    })

    fetch.mockResolvedValueOnce(json({ folder: { id: 'f2' }, files: [] }))
    expect(await api.fetchFolder('f2')).toEqual({ folder: { id: 'f2' }, files: [] })
    expect(fetch).toHaveBeenLastCalledWith('/api/v1/folders/f2', { method: 'GET', credentials: 'same-origin' })

    fetch.mockResolvedValueOnce(json({ folder: { id: 'f2', title: 'Photos' } }))
    expect(await api.renameFolder('f2', 'Photos')).toEqual({ id: 'f2', title: 'Photos' })
    expect(fetch).toHaveBeenLastCalledWith('/api/v1/folders/f2', {
      method: 'PATCH',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: '{"title":"Photos"}',
    })
    fetch.mockResolvedValueOnce(json({}, 400))
    await expect(api.renameFolder('f2', 'x')).rejects.toThrow('Failed to rename folder (400)')

    fetch.mockResolvedValueOnce(json(null, 204))
    expect(await api.deleteFolder('f2')).toBeUndefined()
    expect(fetch).toHaveBeenLastCalledWith('/api/v1/folders/f2', { method: 'DELETE', credentials: 'same-origin' })

    fetch.mockResolvedValueOnce(json({ error: 'not enough credits' }, 402))
    await expect(api.createFolder(30)).rejects.toMatchObject({ message: 'not enough credits', status: 402 })
    fetch.mockResolvedValueOnce(json({}, 500))
    await expect(api.listFolders()).rejects.toThrow('Failed to load folders (500)')
    fetch.mockResolvedValueOnce(json({}, 404))
    await expect(api.fetchFolder('x')).rejects.toThrow('Failed to load folder (404)')
    fetch.mockResolvedValueOnce(json({}, 500))
    await expect(api.deleteFolder('x')).rejects.toThrow('Failed to delete folder (500)')
  })

  it('uploads and deletes files by encoded path', async () => {
    const api = await loadApi('')
    const file = new Blob(['hi'])
    fetch.mockResolvedValueOnce(json({ file: { path: 'a b/c.txt' }, creditsSpent: 2 }))
    expect(await api.uploadFile('f1', 'a b/c.txt', file)).toEqual({ file: { path: 'a b/c.txt' }, creditsSpent: 2 })
    expect(fetch).toHaveBeenLastCalledWith('/api/v1/folders/f1/files/a%20b/c.txt', {
      method: 'PUT',
      credentials: 'same-origin',
      body: file,
    })

    fetch.mockResolvedValueOnce(json({ error: 'file too large' }, 413))
    await expect(api.uploadFile('f1', 'big.bin', file)).rejects.toMatchObject({ message: 'file too large', status: 413 })
    fetch.mockResolvedValueOnce(json({}, 500))
    await expect(api.uploadFile('f1', 'x.txt', file)).rejects.toThrow('Failed to upload x.txt (500)')

    fetch.mockResolvedValueOnce(json(null, 204))
    await api.deleteFile('f1', 'a b/c.txt')
    expect(fetch).toHaveBeenLastCalledWith('/api/v1/folders/f1/files/a%20b/c.txt', {
      method: 'DELETE',
      credentials: 'same-origin',
    })
    fetch.mockResolvedValueOnce(json({}, 404))
    await expect(api.deleteFile('f1', 'x')).rejects.toThrow('Failed to delete file (404)')
  })
})
