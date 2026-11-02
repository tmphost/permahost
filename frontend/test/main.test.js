// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it, vi } from 'vitest'

const mount = vi.fn()
const createpermahostApp = vi.fn(() => ({ app: { mount } }))
vi.mock('../src/app', () => ({ createpermahostApp }))

describe('main', () => {
  it('boots the web app under the injected base path', async () => {
    window.__PERMAHOST_BASE_PATH__ = '/base'
    await import('../src/main.js')
    expect(createpermahostApp).toHaveBeenCalledWith({ history: expect.anything(), platform: 'web' })
    expect(createpermahostApp.mock.calls[0][0].history.base).toBe('/base')
    expect(mount).toHaveBeenCalledWith('#app')
    delete window.__PERMAHOST_BASE_PATH__
  })

  it('defaults the base path to /', async () => {
    vi.resetModules()
    createpermahostApp.mockClear()
    await import('../src/main.js')
    expect(createpermahostApp.mock.calls[0][0].history.base).toBe('')
  })
})
