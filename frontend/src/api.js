// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// The Go server injects the base path the app is served under.
export const BASE_PATH = (window.__TMPHOST_BASE_PATH__ || '').replace(/\/$/, '')

export const API_BASE_URL = `${BASE_PATH}/api/v1`

let userCache = null
let userPromise = null

// fetchUser returns the signed-in user, or null when there is no session.
// The result is cached until clearUserCache() so route changes don't refetch.
export function fetchUser() {
  if (userCache) return Promise.resolve(userCache)
  if (userPromise) return userPromise
  userPromise = (async () => {
    try {
      const res = await fetch(`${API_BASE_URL}/users/me`, { credentials: 'same-origin' })
      if (res.status === 401) return null
      if (!res.ok) throw new Error(`Failed to load user (${res.status})`)
      const body = await res.json()
      userCache = body.user
      return userCache
    } finally {
      userPromise = null
    }
  })()
  return userPromise
}

export function clearUserCache() {
  userCache = null
}

// fetchAuthConfig lists the sign-in providers the server has enabled.
export async function fetchAuthConfig() {
  const res = await fetch(`${BASE_PATH}/auth/config`)
  if (!res.ok) throw new Error(`Failed to load sign-in options (${res.status})`)
  const body = await res.json()
  return body.providers
}

// providerLoginUrl is the server route that starts an OAuth round trip.
// redirect is an in-app path to land on afterwards.
export function providerLoginUrl(provider, redirect) {
  const url = `${BASE_PATH}/auth/${encodeURIComponent(provider)}/login`
  return redirect ? `${url}?redirect_url=${encodeURIComponent(redirect)}` : url
}

export async function logout() {
  try {
    await fetch(`${BASE_PATH}/auth/logout`, { method: 'POST', credentials: 'same-origin' })
  } finally {
    clearUserCache()
  }
}

// httpError builds an Error from an API error body, keeping the status so
// callers can tell an expired session (401) from other failures.
async function httpError(res, fallback) {
  let message = fallback
  try {
    const body = await res.json()
    if (body && body.error) message = body.error
  } catch {
    // Not JSON; use the fallback.
  }
  const err = new Error(message)
  err.status = res.status
  return err
}

// setupLogin exchanges the root access token for a root session cookie.
export async function setupLogin(token) {
  const res = await fetch(`${API_BASE_URL}/setup/login`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ token }),
  })
  if (!res.ok) throw await httpError(res, `Sign-in failed (${res.status})`)
}

export async function setupLogout() {
  await fetch(`${API_BASE_URL}/setup/logout`, { method: 'POST', credentials: 'same-origin' })
}

// fetchSettings returns { settings, secretsSet, restartRequired }, or null
// without a root session.
export async function fetchSettings() {
  const res = await fetch(`${API_BASE_URL}/setup/settings`, { credentials: 'same-origin' })
  if (res.status === 401) return null
  if (!res.ok) throw await httpError(res, `Failed to load settings (${res.status})`)
  return res.json()
}

// saveSettings replaces the settings. Blank secrets keep their stored value.
export async function saveSettings(settings) {
  const res = await fetch(`${API_BASE_URL}/setup/settings`, {
    method: 'PUT',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ settings }),
  })
  if (!res.ok) throw await httpError(res, `Failed to save settings (${res.status})`)
  return res.json()
}

// request sends a JSON request for the signed-in user and returns the parsed
// body, or null for 204. Failures throw an Error carrying the HTTP status.
async function request(method, path, { body, raw, fallback } = {}) {
  const init = { method, credentials: 'same-origin' }
  if (raw !== undefined) {
    init.body = raw
  } else if (body !== undefined) {
    init.headers = { 'Content-Type': 'application/json' }
    init.body = JSON.stringify(body)
  }
  const res = await fetch(`${API_BASE_URL}${path}`, init)
  if (!res.ok) throw await httpError(res, `${fallback} (${res.status})`)
  return res.status === 204 ? null : res.json()
}

// filePath encodes each segment of a folder-relative path, keeping the slashes.
export function filePath(path) {
  return path.split('/').map(encodeURIComponent).join('/')
}

// fetchLimits returns the upload limits and billing block size.
export function fetchLimits() {
  return request('GET', '/limits', { fallback: 'Failed to load limits' })
}

export async function listFolders() {
  const body = await request('GET', '/folders', { fallback: 'Failed to load folders' })
  return body.folders
}

// createFolder charges nothing by itself; credits are spent per uploaded file.
export async function createFolder(ttlMinutes) {
  const body = await request('POST', '/folders', { body: { ttlMinutes }, fallback: 'Failed to create folder' })
  return body.folder
}

// fetchFolder returns { folder, files }.
export function fetchFolder(id) {
  return request('GET', `/folders/${encodeURIComponent(id)}`, { fallback: 'Failed to load folder' })
}

// renameFolder sets a folder's title; an empty title clears it. It returns
// the updated folder.
export async function renameFolder(id, title) {
  const body = await request('PATCH', `/folders/${encodeURIComponent(id)}`, {
    body: { title },
    fallback: 'Failed to rename folder',
  })
  return body.folder
}

export async function deleteFolder(id) {
  await request('DELETE', `/folders/${encodeURIComponent(id)}`, { fallback: 'Failed to delete folder' })
}

// uploadFile stores one file under path and returns { file, creditsSpent }.
export function uploadFile(folderId, path, file) {
  return request('PUT', `/folders/${encodeURIComponent(folderId)}/files/${filePath(path)}`, {
    raw: file,
    fallback: `Failed to upload ${path}`,
  })
}

export async function deleteFile(folderId, path) {
  await request('DELETE', `/folders/${encodeURIComponent(folderId)}/files/${filePath(path)}`, {
    fallback: 'Failed to delete file',
  })
}
