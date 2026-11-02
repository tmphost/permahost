<!--
  Copyright 2026 Contextual, Inc. https://tmphost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
// The tmphost.io mark (see /favicon-light.svg): a ring that is half solid
// (present) and half dashed (expiring), around code brackets.
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
      aria-hidden="true"
    >
      <path d="M12 3a9 9 0 0 0 0 18" />
      <path d="M12 3a9 9 0 0 1 9 9" stroke-dasharray="2.5 3.5" />
      <path d="M21 12a9 9 0 0 1-9 9" stroke-dasharray="1 4" opacity="0.5" />
      <path d="m8 9.5-2.5 2.5 2.5 2.5" />
      <path d="m16 9.5 2.5 2.5-2.5 2.5" />
      <path d="m13.5 7.5-3 9" />
    </svg>
  </span>
</template>
