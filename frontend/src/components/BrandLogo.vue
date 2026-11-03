<!--
  Copyright 2026 Contextual, Inc. https://permahost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
// The permahost.io mark (see /favicon-light.svg): a tiny bird in flight
// under a white sun.
defineProps({
  // tile draws the mark white on a black square, like the favicon.
  tile: { type: Boolean, default: false },
})
</script>

<template>
  <span
    :class="['inline-flex items-center justify-center shrink-0', tile ? 'bg-black dark:bg-white text-white dark:text-zinc-900 rounded-lg' : 'text-gray-900 dark:text-zinc-100']"
    data-test="brand-logo"
  >
    <svg
      viewBox="0 0 24 24"
      :class="tile ? 'w-3/4 h-3/4' : 'w-full h-full'"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      <path d="M2 16q4.75-6 9.5 0q4.75-6 9.5 0" />
      <circle cx="19" cy="8.5" r="2" fill="currentColor" stroke="none" />
    </svg>
  </span>
</template>
