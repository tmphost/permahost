<!--
  Copyright 2026 Contextual, Inc. https://permahost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
// The permahost.io mark (see /favicon-light.svg): a disk drive whose status
// light is green, i.e. online and staying online.
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
      <rect x="2" y="7" width="20" height="10" rx="3" />
      <path d="M6 12h.01M9 12h.01" />
      <circle cx="17" cy="12" r="1.8" class="fill-green-500" stroke="none" />
    </svg>
  </span>
</template>
