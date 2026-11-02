<!--
  Copyright 2026 Contextual, Inc. https://tmphost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
import { computed, onMounted, ref } from 'vue'
import BrandLogo from '../components/BrandLogo.vue'
import { createFolder, fetchLimits, uploadFile } from '../api'
import { useClipboard } from '../composables/useClipboard'
import { useToasts } from '../composables/useToasts'
import { droppedFiles, fileItems, formatBytes, formatMinutes } from '../utils/files'

// Upload without an account: the server knows the visitor by IP address
// and applies the anonymous limits it reports from /limits.

const PRIMARY =
  'rounded-lg bg-black dark:bg-white px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-white dark:text-zinc-900 hover:bg-gray-800 dark:hover:bg-zinc-200 disabled:opacity-50 transition-colors'
const SECONDARY =
  'rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100 hover:border-gray-300 dark:hover:border-zinc-600 disabled:opacity-50 transition-colors'
const ICON_BUTTON =
  'inline-flex items-center justify-center size-7 shrink-0 rounded-lg text-gray-400 dark:text-zinc-500 hover:text-gray-900 dark:hover:text-zinc-100 hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors'
const ICONS = {
  copy: 'M8 8V5a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1h-3M5 8h10a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V9a1 1 0 0 1 1-1z',
  open: 'M14 4h6v6m0-6L10 14M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5',
}

const { notifyError, notifySuccess } = useToasts()
const { copy } = useClipboard()

const limits = ref(null)
const selected = ref([])
const dragging = ref(false)
const uploading = ref(false)
const progress = ref({ done: 0, total: 0 })
// The last upload: { folder, files }.
const result = ref(null)
// Whether the next upload goes into result's folder, replacing files with
// the same path, rather than a new folder.
const addToResult = ref(false)
const replaces = (path) => addToResult.value && result.value.files.some((f) => f.path === path)

const isOversized = (file) => Boolean(limits.value) && file.size > limits.value.maxUploadBytes
const oversized = computed(() => selected.value.filter((item) => isOversized(item.file)))
const tooMany = computed(() => Boolean(limits.value?.fileOps) && selected.value.length > limits.value.fileOps)
const canUpload = computed(
  () =>
    Boolean(limits.value) && selected.value.length > 0 && !oversized.value.length && !tooMany.value && !uploading.value,
)

function addFiles(items) {
  const byPath = new Map(selected.value.map((item) => [item.path, item]))
  for (const item of items) byPath.set(item.path, item)
  selected.value = [...byPath.values()]
}

function onPick(event) {
  addFiles(fileItems(event.target.files))
  event.target.value = ''
}

async function onDrop(event) {
  dragging.value = false
  try {
    addFiles(await droppedFiles(event.dataTransfer))
  } catch (err) {
    notifyError(err.message)
  }
}

function removeSelected(path) {
  selected.value = selected.value.filter((item) => item.path !== path)
}

async function upload() {
  const items = selected.value
  uploading.value = true
  progress.value = { done: 0, total: items.length }
  try {
    if (!addToResult.value) result.value = { folder: await createFolder(limits.value.defaultTtlMinutes), files: [] }
    const { folder, files } = result.value
    for (const item of items) {
      const { file } = await uploadFile(folder.id, item.path, item.file)
      const i = files.findIndex((f) => f.path === file.path)
      if (i < 0) files.push(file)
      else files[i] = file
      progress.value = { done: progress.value.done + 1, total: items.length }
    }
    selected.value = []
    addToResult.value = true
    notifySuccess(items.length === 1 ? 'Uploaded 1 file' : `Uploaded ${items.length} files`)
  } catch (err) {
    notifyError(err.message)
  } finally {
    uploading.value = false
  }
}

onMounted(async () => {
  try {
    limits.value = await fetchLimits()
  } catch (err) {
    notifyError(err.message)
  }
})
</script>

<template>
  <div class="min-h-[100dvh] flex items-center justify-center bg-zinc-50 dark:bg-zinc-950 px-4 py-10">
    <div class="w-full max-w-md bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-sm p-8">
      <div class="text-center mb-6">
        <BrandLogo tile class="w-12 h-12 mx-auto mb-5" />
        <h1 class="text-2xl font-black text-gray-900 dark:text-zinc-100 tracking-tight">Upload without an account</h1>
        <p v-if="limits" class="mt-1.5 text-sm text-gray-500 dark:text-zinc-400" data-test="upload-limits">
          Up to {{ formatBytes(limits.maxUploadBytes) }} per file and {{ limits.fileOps }} files every
          {{ formatMinutes(limits.fileOpsWindowMinutes) }}. Links expire after
          {{ formatMinutes(limits.defaultTtlMinutes) }}.
        </p>
      </div>

      <div
        :class="[
          'rounded-xl border border-dashed p-5 text-center transition-colors',
          dragging ? 'border-gray-400 dark:border-zinc-500 bg-zinc-50 dark:bg-zinc-950' : 'border-gray-200 dark:border-zinc-800 hover:border-gray-300 dark:hover:border-zinc-600',
        ]"
        data-test="upload-card"
        @dragover.prevent="dragging = true"
        @dragleave="dragging = false"
        @drop.prevent="onDrop"
      >
        <p class="text-xs text-gray-500 dark:text-zinc-400">Drop files here, or</p>
        <div class="mt-3 flex flex-wrap justify-center gap-2">
          <label :class="[SECONDARY, 'cursor-pointer']">
            Choose files
            <input type="file" multiple class="sr-only" data-test="pick-files" @change="onPick" />
          </label>
          <label :class="[SECONDARY, 'cursor-pointer']">
            Choose folder
            <input type="file" webkitdirectory multiple class="sr-only" data-test="pick-folder" @change="onPick" />
          </label>
        </div>
      </div>

      <div v-if="selected.length" class="mt-4" data-test="upload-summary">
        <ul class="max-h-48 overflow-y-auto no-scrollbar divide-y divide-gray-50 dark:divide-zinc-800">
          <li
            v-for="item in selected"
            :key="item.path"
            class="flex items-center gap-3 py-1.5 text-xs"
            :data-test="`selected-${item.path}`"
          >
            <span class="grow min-w-0 truncate font-mono text-gray-900 dark:text-zinc-100">{{ item.path }}</span>
            <span
              v-if="replaces(item.path)"
              class="shrink-0 text-[10px] font-black uppercase tracking-widest text-amber-600 dark:text-amber-400"
              data-test="selected-replaces"
            >
              Replaces
            </span>
            <span :class="['tabular-nums', isOversized(item.file) ? 'font-bold text-rose-600 dark:text-rose-400' : 'text-gray-400 dark:text-zinc-500']">
              {{ formatBytes(item.file.size) }}
            </span>
            <button
              type="button"
              :class="ICON_BUTTON"
              :title="`Remove ${item.path}`"
              data-test="selected-remove"
              @click="removeSelected(item.path)"
            >
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M6 18 18 6M6 6l12 12" />
              </svg>
            </button>
          </li>
        </ul>

        <p v-if="oversized.length" role="alert" class="mt-3 text-xs font-medium text-rose-600 dark:text-rose-400" data-test="upload-oversized">
          {{ oversized.length === 1 ? '1 file is' : `${oversized.length} files are` }} over the
          {{ formatBytes(limits.maxUploadBytes) }} limit. Sign in to upload larger files.
        </p>
        <p v-if="tooMany" role="alert" class="mt-3 text-xs font-medium text-rose-600 dark:text-rose-400" data-test="upload-too-many">
          At most {{ limits.fileOps }} files every {{ formatMinutes(limits.fileOpsWindowMinutes) }} without an account.
        </p>

        <label v-if="result" class="mt-3 flex items-center gap-2 text-xs text-gray-500 dark:text-zinc-400">
          <input v-model="addToResult" type="checkbox" class="accent-black dark:accent-white" data-test="upload-into-result" />
          Add to the folder below, replacing files with the same path
        </label>

        <div class="mt-4 flex items-center justify-end gap-3">
          <button type="button" :disabled="uploading" :class="SECONDARY" data-test="upload-clear" @click="selected = []">
            Clear
          </button>
          <button type="button" :disabled="!canUpload" :class="PRIMARY" data-test="upload-start" @click="upload">
            {{ uploading ? `Uploading ${progress.done} / ${progress.total}` : 'Upload' }}
          </button>
        </div>
      </div>

      <div v-if="result" class="mt-6 rounded-xl border border-gray-100 dark:border-zinc-800" data-test="upload-result">
        <div class="flex items-center gap-2 px-3 py-2 border-b border-gray-100 dark:border-zinc-800">
          <span class="grow min-w-0 truncate font-mono text-xs font-bold text-gray-900 dark:text-zinc-100" :title="result.folder.url">
            {{ result.folder.url }}
          </span>
          <button
            type="button"
            :class="ICON_BUTTON"
            title="Copy folder URL"
            data-test="result-folder-copy"
            @click="copy(result.folder.url, 'Folder URL copied')"
          >
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
              <path stroke-linecap="round" stroke-linejoin="round" :d="ICONS.copy" />
            </svg>
          </button>
          <a :href="result.folder.url" target="_blank" rel="noopener" :class="ICON_BUTTON" title="Open folder">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
              <path stroke-linecap="round" stroke-linejoin="round" :d="ICONS.open" />
            </svg>
          </a>
        </div>
        <ul class="divide-y divide-gray-50 dark:divide-zinc-800 px-3 py-1">
          <li v-for="file in result.files" :key="file.path" class="flex items-center gap-2 py-1" :data-test="`result-${file.path}`">
            <span class="grow min-w-0 truncate font-mono text-xs text-gray-900 dark:text-zinc-100">{{ file.path }}</span>
            <span class="shrink-0 text-[11px] tabular-nums text-gray-400 dark:text-zinc-500">{{ formatBytes(file.size) }}</span>
            <button
              type="button"
              :class="ICON_BUTTON"
              title="Copy file URL"
              data-test="result-file-copy"
              @click="copy(file.url, 'File URL copied')"
            >
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                <path stroke-linecap="round" stroke-linejoin="round" :d="ICONS.copy" />
              </svg>
            </button>
          </li>
        </ul>
      </div>

      <div class="mt-8 pt-5 border-t border-gray-100 dark:border-zinc-800 flex items-center justify-between text-[11px] font-bold text-gray-400 dark:text-zinc-500">
        <span>&copy; {{ new Date().getFullYear() }} tmphost.io</span>
        <router-link to="/login" class="hover:text-gray-900 dark:hover:text-zinc-100 transition-colors" data-test="upload-sign-in">
          Sign in for more
        </router-link>
      </div>
    </div>
  </div>
</template>
