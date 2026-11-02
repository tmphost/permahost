// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { defineStore } from 'pinia'

const PLATFORMS = ['web', 'desktop']

// The supported seam for behavior only one build can offer.
export const usePlatformStore = defineStore('platform', {
  state: () => ({ platform: 'web' }),
  getters: {
    isDesktop: (s) => s.platform === 'desktop',
    isWeb: (s) => s.platform === 'web',
  },
  actions: {
    setPlatform(next) {
      this.platform = PLATFORMS.includes(next) ? next : 'web'
    },
  },
})
