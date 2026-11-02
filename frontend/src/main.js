// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { createWebHistory } from 'vue-router'
import { createpermahostApp } from './app'

const { app } = createpermahostApp({
  history: createWebHistory(window.__PERMAHOST_BASE_PATH__ || '/'),
  platform: 'web',
})
app.mount('#app')
