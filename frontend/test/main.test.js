// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it, vi } from 'vitest'

const mount = vi.fn()
const createtmphostApp = vi.fn(() => ({ app: { mount } }))
vi.mock('../src/app', () => ({ createtmphostApp }))

describe('main', () => {
  it('boots the web app under the injected base path', async () => {
    window.__TMPHOST_BASE_PATH__ = '/base'
    await import('../src/main.js')
    expect(createtmphostApp).toHaveBeenCalledWith({ history: expect.anything(), platform: 'web' })
    expect(createtmphostApp.mock.calls[0][0].history.base).toBe('/base')
    expect(mount).toHaveBeenCalledWith('#app')
    delete window.__TMPHOST_BASE_PATH__
  })

  it('defaults the base path to /', async () => {
    vi.resetModules()
    createtmphostApp.mockClear()
    await import('../src/main.js')
    expect(createtmphostApp.mock.calls[0][0].history.base).toBe('')
  })
})
