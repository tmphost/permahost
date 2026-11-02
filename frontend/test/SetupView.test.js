// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

vi.mock('../src/api', () => ({
  fetchSettings: vi.fn(),
  saveSettings: vi.fn(),
  setupLogin: vi.fn(),
  setupLogout: vi.fn(),
}))

import { fetchSettings, saveSettings, setupLogin, setupLogout } from '../src/api'
import SetupView from '../src/views/SetupView.vue'
import { useToasts } from '../src/composables/useToasts'

function state(overrides = {}) {
  return {
    settings: {
      domain: { appUrl: 'https://app.tmphost.io/', sitesDomain: 'tmphost.io' },
      tls: { enabled: false, port: 443, email: '', lego: { enabled: false, cloudflareApiToken: '' } },
      credits: { monthlyGrant: 250 },
      database: {
        driver: 'sqlite',
        sqlite: { dsn: './_storage/tmphost.db' },
        postgres: { dsn: '' },
      },
      storage: {
        driver: 'local',
        local: { path: './_storage/files' },
        s3: { endpoint: '', region: '', bucket: '', accessKeyId: '', secretAccessKey: '', usePathStyle: false },
      },
      files: { maxTtlMinutes: 432000, maxUploadMb: 16, anonymousMaxTtlMinutes: 10, subdomains: false, publicDomain: '' },
      auth: {
        google: { clientId: 'g', clientSecret: '' },
        github: { clientId: '', clientSecret: '' },
        providers: [{ id: 'my-okta', name: 'My Okta', icon: '<svg></svg>', clientId: 'oc', clientSecret: '', authUrl: 'https://okta.example.com' }],
      },
    },
    secretsSet: {
      postgresDsn: true,
      s3SecretAccessKey: false,
      googleClientSecret: true,
      githubClientSecret: false,
      cloudflareApiToken: false,
      providerClientSecrets: { 'my-okta': true },
    },
    restartRequired: false,
    ...overrides,
  }
}

async function mountView() {
  const wrapper = mount(SetupView)
  await flushPromises()
  return wrapper
}

describe('SetupView', () => {
  beforeEach(() => {
    useToasts().toasts.value = []
  })
  afterEach(() => {
    useToasts().toasts.value = []
  })

  it('shows a spinner while loading', () => {
    fetchSettings.mockReturnValue(new Promise(() => {}))
    expect(mount(SetupView).find('[data-test="setup-loading"]').exists()).toBe(true)
  })

  it('asks for the root token without a session, then loads settings', async () => {
    fetchSettings.mockResolvedValueOnce(null).mockResolvedValueOnce(state())
    setupLogin.mockResolvedValue()
    const wrapper = await mountView()
    expect(wrapper.find('[data-test="setup-login"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="setup-unlock"]').attributes('disabled')).toBeDefined()

    await wrapper.find('[data-test="setup-token"]').setValue('root-token')
    await wrapper.find('[data-test="setup-login"]').trigger('submit')
    await flushPromises()
    expect(setupLogin).toHaveBeenCalledWith('root-token')
    expect(wrapper.find('[data-test="setup-form"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="setup-grant"]').element.value).toBe('250')
  })

  it('shows a wrong-token error', async () => {
    fetchSettings.mockResolvedValue(null)
    setupLogin.mockRejectedValue(new Error('invalid root access token'))
    const wrapper = await mountView()
    await wrapper.find('[data-test="setup-token"]').setValue('nope')
    await wrapper.find('[data-test="setup-login"]').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[data-test="setup-login-error"]').text()).toBe('invalid root access token')
  })

  it('falls back to sign-in when loading fails', async () => {
    fetchSettings.mockRejectedValue(new Error('Failed to load settings (500)'))
    const wrapper = await mountView()
    expect(wrapper.find('[data-test="setup-login"]').exists()).toBe(true)
    expect(useToasts().toasts.value[0].message).toBe('Failed to load settings (500)')
  })

  it('renders sections, masks saved secrets and switches database driver', async () => {
    fetchSettings.mockResolvedValue(state())
    const wrapper = await mountView()
    expect(wrapper.find('[data-test="setup-sqlite"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="setup-restart"]').exists()).toBe(false)

    await wrapper.find('[data-test="setup-driver-postgres"]').trigger('click')
    expect(wrapper.find('[data-test="setup-postgres"]').exists()).toBe(true)
    expect(wrapper.find('#pg-dsn').attributes('placeholder')).toContain('saved')
    expect(wrapper.find('#pg-dsn').attributes('type')).toBe('password')
    expect(wrapper.find('#google-secret').attributes('placeholder')).toContain('saved')
    expect(wrapper.find('#github-secret').attributes('placeholder')).toBe('Not set')

    await wrapper.find('[data-test="setup-driver-sqlite"]').trigger('click')
    expect(wrapper.find('[data-test="setup-sqlite"]').exists()).toBe(true)
  })

  it('shows an example DSN when none is saved', async () => {
    fetchSettings.mockResolvedValue(state({ secretsSet: { ...state().secretsSet, postgresDsn: false } }))
    const wrapper = await mountView()
    await wrapper.find('[data-test="setup-driver-postgres"]').trigger('click')
    expect(wrapper.find('#pg-dsn').attributes('placeholder')).toMatch(/^postgres:\/\//)
  })

  it('saves the edited settings and shows restart notice', async () => {
    fetchSettings.mockResolvedValue(state())
    saveSettings.mockImplementation(async (settings) => state({ settings: { ...settings, auth: { ...settings.auth, github: { clientId: 'h', clientSecret: '' } } }, restartRequired: true }))
    const wrapper = await mountView()

    await wrapper.find('[data-test="setup-grant"]').setValue('400')
    await wrapper.find('[data-test="setup-storage-s3"]').trigger('click')
    await wrapper.find('#s3-bucket').setValue('files')
    await wrapper.find('[data-test="setup-max-ttl"]').setValue('1440')
    await wrapper.find('[data-test="setup-max-upload"]').setValue('64')
    await wrapper.find('[data-test="setup-anonymous-max-ttl"]').setValue('5')
    await wrapper.find('#github-id').setValue('h')
    await wrapper.find('#github-secret').setValue('hsecret')
    await wrapper.find('[data-test="setup-form"]').trigger('submit')
    await flushPromises()

    const sent = saveSettings.mock.calls[0][0]
    expect(sent.credits.monthlyGrant).toBe(400)
    expect(sent.storage.driver).toBe('s3')
    expect(sent.storage.s3).toMatchObject({ bucket: 'files' })
    expect(sent.storage.s3).not.toHaveProperty('enabled')
    expect(sent.files).toEqual({ maxTtlMinutes: 1440, maxUploadMb: 64, anonymousMaxTtlMinutes: 5, subdomains: false, publicDomain: '' })
    expect(sent.auth.github).toEqual({ clientId: 'h', clientSecret: 'hsecret' })
    expect(wrapper.find('[data-test="setup-restart"]').exists()).toBe(true)
    expect(wrapper.find('#github-secret').element.value).toBe('', 'secret cleared after save')
    expect(useToasts().toasts.value.at(-1)).toMatchObject({ type: 'success', message: 'Settings saved' })
  })

  it('binds every field', async () => {
    fetchSettings.mockResolvedValue(state())
    saveSettings.mockImplementation(async (settings) => state({ settings }))
    const wrapper = await mountView()
    await wrapper.find('#sqlite-dsn').setValue('/data/x.db')
    await wrapper.find('#local-path').setValue('/srv/files')
    await wrapper.find('[data-test="setup-driver-postgres"]').trigger('click')
    await wrapper.find('[data-test="setup-storage-s3"]').trigger('click')
    const values = {
      '#pg-dsn': 'postgres://app:pw@db:6543/prod?sslmode=require',
      '#s3-endpoint': 'https://s3.example.com', '#s3-region': 'auto', '#s3-key': 'AK', '#s3-secret': 'SK',
      '#google-id': 'g2', '#google-secret': 'gs2',
    }
    for (const [sel, value] of Object.entries(values)) await wrapper.find(sel).setValue(value)
    await wrapper.find('[data-test="setup-s3-path-style"]').setValue(true)
    await wrapper.find('[data-test="setup-form"]').trigger('submit')
    await flushPromises()

    expect(saveSettings.mock.calls[0][0]).toMatchObject({
      database: {
        driver: 'postgres',
        sqlite: { dsn: '/data/x.db' },
        postgres: { dsn: 'postgres://app:pw@db:6543/prod?sslmode=require' },
      },
      storage: {
        driver: 's3',
        local: { path: '/srv/files' },
        s3: { endpoint: 'https://s3.example.com', region: 'auto', accessKeyId: 'AK', secretAccessKey: 'SK', usePathStyle: true },
      },
      auth: { google: { clientId: 'g2', clientSecret: 'gs2' } },
    })
  })

  it('chooses path or subdomain public URLs and saves the public domain', async () => {
    fetchSettings.mockResolvedValue(state())
    saveSettings.mockImplementation(async (settings) => state({ settings }))
    const wrapper = await mountView()
    expect(wrapper.find('[data-test="setup-files-help"]').text()).toContain('public URLs apply immediately')
    expect(wrapper.find('[role="radiogroup"][aria-labelledby="public-urls-label"]').exists()).toBe(true)
    expect(wrapper.find('#public-urls-label').text()).toBe('Public URLs')
    expect(wrapper.find('[data-test="setup-urls-path"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.find('[data-test="setup-urls-subdomains"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.find('[data-test="setup-path"]').text()).toContain('/files/<userId>/<folderId>/')
    expect(wrapper.find('[data-test="setup-subdomains"]').exists()).toBe(false)

    await wrapper.find('[data-test="setup-urls-subdomains"]').trigger('click')
    expect(wrapper.find('[data-test="setup-urls-subdomains"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.find('[data-test="setup-path"]').exists()).toBe(false)
    const help = wrapper.find('[data-test="setup-subdomains-help"]').text()
    expect(help).toContain('f<folderId>.<public domain>')
    expect(help).toContain('11 characters')
    expect(help).toContain('blank to use the sites domain')
    expect(help).not.toContain('<userId>')
    expect(wrapper.find('[data-test="setup-subdomains"]').text()).toContain('wildcard DNS')
    const domain = wrapper.find('[data-test="setup-public-domain"]')
    expect(domain.attributes('placeholder')).toBe('tmphost.io')
    expect(wrapper.find('label[for="public-domain"]').text()).toBe('Public domain')
    await domain.setValue(' files.example.com ')
    await wrapper.find('[data-test="setup-form"]').trigger('submit')
    await flushPromises()
    expect(saveSettings.mock.calls.at(-1)[0].files).toEqual({
      maxTtlMinutes: 432000,
      maxUploadMb: 16,
      anonymousMaxTtlMinutes: 10,
      subdomains: true,
      publicDomain: 'files.example.com',
    })

    await wrapper.find('[data-test="setup-urls-path"]').trigger('click')
    expect(wrapper.find('[data-test="setup-subdomains"]').exists()).toBe(false)
    wrapper.unmount()

    fetchSettings.mockResolvedValue(state({ settings: { ...state().settings, files: { maxTtlMinutes: 60, maxUploadMb: 8, subdomains: true, publicDomain: 'tmp.example.org' } } }))
    const loaded = await mountView()
    expect(loaded.find('[data-test="setup-urls-subdomains"]').attributes('aria-checked')).toBe('true')
    expect(loaded.find('[data-test="setup-public-domain"]').element.value).toBe('tmp.example.org')
  })

  it('switches storage driver and keeps the S3 secret write-only', async () => {
    fetchSettings.mockResolvedValue(state())
    const wrapper = await mountView()
    expect(wrapper.find('[data-test="setup-local"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="setup-s3"]').exists()).toBe(false)
    expect(wrapper.find('#local-path').element.value).toBe('./_storage/files')
    expect(wrapper.find('[data-test="setup-storage-local"]').attributes('aria-checked')).toBe('true')

    await wrapper.find('[data-test="setup-storage-s3"]').trigger('click')
    expect(wrapper.find('[data-test="setup-local"]').exists()).toBe(false)
    expect(wrapper.find('#s3-secret').attributes('type')).toBe('password')
    expect(wrapper.find('#s3-secret').attributes('placeholder')).toBe('Not set')
    wrapper.unmount()

    fetchSettings.mockResolvedValue(state({ secretsSet: { ...state().secretsSet, s3SecretAccessKey: true } }))
    const saved = await mountView()
    await saved.find('[data-test="setup-storage-s3"]').trigger('click')
    expect(saved.find('#s3-secret').attributes('placeholder')).toContain('saved')
    expect(saved.find('#s3-secret').element.value).toBe('')
  })

  it('explains the max TTL in hours or days', async () => {
    fetchSettings.mockResolvedValue(state())
    const wrapper = await mountView()
    const hint = () => wrapper.find('[data-test="setup-max-ttl-hint"]').text()
    expect(hint()).toBe('= 300 days')
    await wrapper.find('[data-test="setup-max-ttl"]').setValue('90')
    expect(hint()).toBe('= 1 h 30 min')
    await wrapper.find('[data-test="setup-max-ttl"]').setValue('0')
    expect(hint()).toMatch(/whole number/)
    await wrapper.find('[data-test="setup-max-ttl"]').setValue('')
    expect(hint()).toMatch(/whole number/)

    const anonymous = wrapper.find('[data-test="setup-anonymous-max-ttl"]')
    expect(anonymous.element.value).toBe('10')
    expect(wrapper.find('[data-test="setup-anonymous-max-ttl-hint"]').text()).toBe(
      '= 10 min, for uploads from the public Upload page. Default 10.',
    )
    await wrapper.find('[data-test="setup-max-ttl"]').setValue('60')
    expect(anonymous.attributes('max')).toBe('60')
    expect(wrapper.find('[data-test="setup-max-upload"]').element.value).toBe('16')
  })

  it('shows validation errors from the server', async () => {
    fetchSettings.mockResolvedValue(state())
    saveSettings.mockRejectedValue(Object.assign(new Error('invalid settings: sqlite DSN is required'), { status: 400 }))
    const wrapper = await mountView()
    await wrapper.find('[data-test="setup-form"]').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[data-test="setup-save-error"]').text()).toContain('sqlite DSN is required')
    expect(useToasts().toasts.value.at(-1).type).toBe('error')
  })

  it('returns to sign-in when the root session expired', async () => {
    fetchSettings.mockResolvedValue(state())
    saveSettings.mockRejectedValue(Object.assign(new Error('unauthorized'), { status: 401 }))
    const wrapper = await mountView()
    await wrapper.find('[data-test="setup-form"]').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[data-test="setup-login-error"]').text()).toContain('session expired')
  })

  it('edits domains and TLS', async () => {
    fetchSettings.mockResolvedValue(state())
    saveSettings.mockImplementation(async (settings) => state({ settings }))
    const wrapper = await mountView()
    const tls = wrapper.find('[data-test="setup-tls"]')
    expect(tls.find('.opacity-50').exists()).toBe(true)
    expect(wrapper.find('#tls-cert').exists()).toBe(false)
    expect(wrapper.find('#lego-token').attributes('placeholder')).toBe('Optional')
    expect(tls.text()).toContain('*.tmphost.io')

    await wrapper.find('#app-url').setValue('https://example.org')
    await wrapper.find('#sites-domain').setValue('')
    expect(tls.text()).toContain('*.sites domain')
    await wrapper.find('[data-test="setup-tls-enabled"]').setValue(true)
    await wrapper.find('[data-test="setup-lego-enabled"]').setValue(true)
    expect(tls.find('.opacity-50').exists()).toBe(false)
    const values = {
      '#tls-port': '8443', '#tls-email': 'ops@example.org', '#lego-token': 'cf',
    }
    for (const [sel, value] of Object.entries(values)) await wrapper.find(sel).setValue(value)
    await wrapper.find('[data-test="setup-form"]').trigger('submit')
    await flushPromises()

    expect(saveSettings.mock.calls[0][0]).toMatchObject({
      domain: { appUrl: 'https://example.org', sitesDomain: '' },
      tls: { enabled: true, port: 8443, email: 'ops@example.org', lego: { enabled: true, cloudflareApiToken: 'cf' } },
    })
  })

  it('shows a saved Cloudflare token as kept', async () => {
    const saved = state()
    saved.secretsSet.cloudflareApiToken = true
    fetchSettings.mockResolvedValue(saved)
    const wrapper = await mountView()
    expect(wrapper.find('#lego-token').attributes('placeholder')).toContain('saved')
  })

  it('adds, edits, uploads icons for and removes custom providers', async () => {
    fetchSettings.mockResolvedValue(state())
    saveSettings.mockImplementation(async (settings) => state({ settings }))
    const wrapper = await mountView()

    expect(wrapper.find('#cp-0-secret').attributes('placeholder')).toContain('saved')
    expect(wrapper.find('[data-test="cp-0-callback"]').text()).toContain('https://app.tmphost.io/auth/my-okta/callback')
    expect(wrapper.find('[data-test="cp-0-preview"]').attributes('src')).toBe(`data:image/svg+xml;charset=utf-8,${encodeURIComponent('<svg></svg>')}`)

    await wrapper.find('[data-test="setup-add-provider"]').trigger('click')
    expect(wrapper.find('[data-test="cp-1-preview"]').exists()).toBe(false)
    expect(wrapper.find('#cp-1-secret').attributes('placeholder')).toBe('Not set')
    expect(wrapper.find('[data-test="cp-1-callback"]').text()).toContain('/auth//callback')
    const values = {
      '#cp-1-name': ' Acme  SSO! ', '#cp-1-auth-url': 'https://sso.acme.dev', '#cp-1-id': 'aid', '#cp-1-secret': 'asecret',
    }
    for (const [sel, value] of Object.entries(values)) await wrapper.find(sel).setValue(value)
    expect(wrapper.find('[data-test="cp-1-callback"]').text()).toContain('/auth/acme-sso/callback')

    // Upload reads the file into the icon; an empty pick changes nothing.
    const upload = wrapper.find('[data-test="cp-1-upload"]')
    const svg = '<svg viewBox="0 0 1 1"></svg>'
    Object.defineProperty(upload.element, 'files', { value: [{ text: async () => svg }], configurable: true })
    await upload.trigger('change')
    await flushPromises()
    expect(wrapper.find('#cp-1-icon').element.value).toBe(svg)
    Object.defineProperty(upload.element, 'files', { value: [], configurable: true })
    await upload.trigger('change')
    await flushPromises()
    expect(wrapper.find('#cp-1-icon').element.value).toBe(svg)
    await wrapper.find('#cp-1-icon').setValue('<svg>edited</svg>')

    await wrapper.find('[data-test="cp-0-remove"]').trigger('click')
    expect(wrapper.find('[data-test="setup-custom-provider-1"]').exists()).toBe(false)
    await wrapper.find('[data-test="setup-form"]').trigger('submit')
    await flushPromises()

    expect(saveSettings.mock.calls[0][0].auth.providers).toEqual([
      { name: ' Acme  SSO! ', icon: '<svg>edited</svg>', clientId: 'aid', clientSecret: 'asecret', authUrl: 'https://sso.acme.dev' },
    ])
  })

  it('treats missing providers and provider secrets as empty', async () => {
    const s = state()
    s.settings.auth.providers = null
    delete s.secretsSet.providerClientSecrets
    fetchSettings.mockResolvedValue(s)
    const wrapper = await mountView()
    expect(wrapper.find('[data-test="setup-custom-provider-0"]').exists()).toBe(false)
    await wrapper.find('[data-test="setup-add-provider"]').trigger('click')
    expect(wrapper.find('#cp-0-secret').attributes('placeholder')).toBe('Not set')
  })

  it('signs out, even if the request fails', async () => {
    for (const fail of [false, true]) {
      fetchSettings.mockResolvedValue(state())
      if (fail) setupLogout.mockRejectedValueOnce(new Error('offline'))
      else setupLogout.mockResolvedValueOnce()
      const wrapper = await mountView()
      await wrapper.find('[data-test="setup-logout"]').trigger('click')
      await flushPromises()
      expect(wrapper.find('[data-test="setup-login"]').exists()).toBe(true)
      wrapper.unmount()
    }
  })
})
