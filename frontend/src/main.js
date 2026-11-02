// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { createWebHistory } from 'vue-router'
import { createtmphostApp } from './app'

const { app } = createtmphostApp({
  history: createWebHistory(window.__TMPHOST_BASE_PATH__ || '/'),
  platform: 'web',
})
app.mount('#app')
