// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

// The Go server proxies nothing in development; Vite forwards the API and the
// sign-in routes to it.
// Point TMPHOSTIO_BASE_URL at this dev server so OAuth callbacks land here and
// the session cookie is set on the same origin as the app.
const apiTarget = process.env.TMPHOSTIO_DEV_API_TARGET || 'http://localhost:2027'

export default defineConfig({
  // Relative asset URLs; the Go server rewrites them under its base path.
  base: '',
  plugins: [vue(), tailwindcss()],
  server: {
    proxy: {
      '/api': { target: apiTarget },
      '/auth': { target: apiTarget },
    },
  },
})
