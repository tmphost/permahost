// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { createRouter } from 'vue-router'
import App from './App.vue'
import { fetchUser } from './api'
import { usePlatformStore } from './stores/platformStore'
import { useThemeStore } from './stores/themeStore'
import './style.css'

// The one route table. Never add a route anywhere else (see AGENTS.md).
export const routes = [
  {
    path: '/login',
    name: 'login',
    component: () => import('./views/LoginView.vue'),
    meta: { public: true, guestOnly: true, title: 'Sign in' },
  },
  {
    // Anonymous uploads, with the lower limits the server applies by IP.
    path: '/upload',
    name: 'upload',
    component: () => import('./views/UploadView.vue'),
    meta: { public: true, guestOnly: true, title: 'Upload' },
  },
  {
    // Root-only: the view asks for the root access token itself.
    path: '/setup',
    name: 'setup',
    component: () => import('./views/SetupView.vue'),
    meta: { public: true, title: 'Setup' },
  },
  {
    // Files is the home page: signed-in users land here.
    path: '/',
    name: 'files',
    component: () => import('./views/FilesView.vue'),
    meta: { title: 'Files' },
  },
  {
    path: '/connect',
    name: 'connect',
    component: () => import('./views/ConnectView.vue'),
    meta: { title: 'Connect' },
  },
  { path: '/:pathMatch(.*)*', redirect: '/' },
]

export function createAuthGuard(fetchUserFn = fetchUser) {
  return async function authGuard(to) {
    let user
    try {
      user = await fetchUserFn()
    } catch {
      user = null
    }
    if (to.meta.public) {
      // A signed-in user has no business on the login page.
      return to.meta.guestOnly && user ? '/' : true
    }
    if (user) return true
    return to.fullPath === '/' ? '/login' : { path: '/login', query: { redirect: to.fullPath } }
  }
}

export const clickOutside = {
  mounted(el, binding) {
    el._clickOutside = (event) => {
      if (!el.contains(event.target)) binding.value(event)
    }
    document.addEventListener('click', el._clickOutside)
  },
  unmounted(el) {
    document.removeEventListener('click', el._clickOutside)
  },
}

export function createtmphostApp({ history, platform = 'web' } = {}) {
  const router = createRouter({ history, routes })
  router.beforeEach(createAuthGuard())
  router.afterEach((to) => {
    document.title = to.meta.title ? `${to.meta.title} · tmphost.io` : 'tmphost.io'
  })

  const pinia = createPinia()
  const app = createApp(App)
  app.directive('click-outside', clickOutside)
  app.use(pinia)
  app.use(router)
  usePlatformStore(pinia).setPlatform(platform)
  useThemeStore(pinia).init()
  return { app, router, pinia }
}
