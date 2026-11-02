<!--
  Copyright 2026 Contextual, Inc. https://tmphost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
import { computed, inject, nextTick, onMounted, onUnmounted, ref } from 'vue'
import DeleteModal from '../components/DeleteModal.vue'
import { createFolder, deleteFile, deleteFolder, fetchFolder, fetchLimits, listFolders, renameFolder, uploadFile } from '../api'
import { useClipboard } from '../composables/useClipboard'
import { useToasts } from '../composables/useToasts'
import {
  droppedFiles,
  expiresIn,
  fileItems,
  formatBytes,
  formatMinutes,
  isFileDrag,
  ttlOptions,
  uploadCost,
} from '../utils/files'

const TICK_MS = 30 * 1000
const MINUTE_MS = 60 * 1000

const PRIMARY =
  'rounded-lg bg-black dark:bg-white px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-white dark:text-zinc-900 hover:bg-gray-800 dark:hover:bg-zinc-200 disabled:opacity-50 transition-colors'
const SECONDARY =
  'rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100 hover:border-gray-300 dark:hover:border-zinc-600 disabled:opacity-50 transition-colors'
const ICON_BUTTON =
  'inline-flex items-center justify-center size-7 shrink-0 rounded-lg text-gray-400 dark:text-zinc-500 hover:text-gray-900 dark:hover:text-zinc-100 hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors'
const ICONS = {
  copy: 'M8 8V5a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1h-3M5 8h10a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V9a1 1 0 0 1 1-1z',
  open: 'M14 4h6v6m0-6L10 14M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5',
  upload: 'M12 16V4m0 0L7 9m5-5 5 5M4 16v3a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-3',
  trash: 'M4 7h16M10 11v6m4-6v6M6 7l1 12a1 1 0 0 0 1 1h8a1 1 0 0 0 1-1l1-12M9 7V4h6v3',
}

const { notifyError, notifySuccess } = useToasts()
const { copy } = useClipboard()
const currentUser = inject('currentUser', { user: ref(null), refreshUser: async () => {} })

const limits = ref(null)
const folders = ref([])
const loading = ref(true)
const now = ref(Date.now())
let timer = null

// Upload form
const selected = ref([])
const ttl = ref(30)
const dragging = ref(false)
// dragenter/dragleave fire for every child the pointer crosses; count them so
// the drop overlay only hides once the drag has left the page.
let dragDepth = 0
const uploading = ref(false)
const progress = ref({ done: 0, total: 0 })
// The ID of the existing folder uploads go into, or null for a new folder.
// Its files keep their links: an upload to a path already there replaces
// the file.
const targetId = ref(null)
// The paths already in target, to mark the files an upload replaces.
const targetPaths = ref(new Set())
const uploadCard = ref(null)

// Expanded folders: id -> { loading, files }
const expanded = ref({})

// The folder title being edited: { id, value }.
const editing = ref(null)
// The title input; a ref inside v-for is an array, of one input here.
const titleInput = ref([])

// The pending deletion: { folder } or { folder, file }.
const pendingDelete = ref(null)
const deleting = ref(false)

const ttlChoices = computed(() => (limits.value ? ttlOptions(limits.value.maxTtlMinutes) : []))
// The slider steps through ttlChoices by index; ttl keeps the minutes. It
// binds :value rather than v-model so Vue sets max before the value.
const ttlIndex = computed(() => Math.max(0, ttlChoices.value.indexOf(ttl.value)))
const sortedFolders = computed(() =>
  [...folders.value].sort((a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt)),
)
// remainingMinutes is what is left of a folder's lifetime, in started minutes.
const remainingMinutes = (folder) => Math.max(0, Math.ceil((Date.parse(folder.expiresAt) - now.value) / MINUTE_MS))
// target is looked up by ID so it follows renames and reloads.
const target = computed(() => folders.value.find((f) => f.id === targetId.value) ?? null)
const targetExpired = computed(() => Boolean(target.value) && remainingMinutes(target.value) === 0)
const replacing = computed(() => selected.value.filter((item) => targetPaths.value.has(item.path)).length)
const isOversized = (file) => Boolean(limits.value) && file.size > limits.value.maxUploadBytes
const oversized = computed(() => selected.value.filter((item) => isOversized(item.file)))
const cost = computed(() =>
  limits.value
    ? uploadCost(
        selected.value.map((item) => item.file.size),
        // Files added to a folder pay for the lifetime it has left.
        target.value ? remainingMinutes(target.value) : ttl.value,
        limits.value.billingBlockMinutes,
        limits.value.billingUnitBytes,
      )
    : 0,
)
const credits = computed(() => currentUser.user.value?.credits)
const notEnoughCredits = computed(() => credits.value !== undefined && cost.value > credits.value)
const canUpload = computed(
  () =>
    Boolean(limits.value) &&
    selected.value.length > 0 &&
    oversized.value.length === 0 &&
    !targetExpired.value &&
    !uploading.value,
)

// folderTitle is what a folder is called: its title, else the name of its
// only file, else its ID.
const folderTitle = (folder) => folder.title || folder.defaultTitle || folder.id

const deleteTitle = computed(() => (pendingDelete.value?.file ? 'Delete file?' : 'Delete folder?'))
const deleteMessage = computed(() => {
  const target = pendingDelete.value
  if (!target) return ''
  const name = folderTitle(target.folder)
  if (target.file) return `${target.file.path} is removed from folder ${name}. Its link stops working.`
  return `Folder ${name} and all of its files are removed. Their links stop working.`
})

async function loadLimits() {
  try {
    limits.value = await fetchLimits()
    const choices = ttlChoices.value
    ttl.value = choices.includes(limits.value.defaultTtlMinutes) ? limits.value.defaultTtlMinutes : choices[0]
  } catch (err) {
    notifyError(err.message)
  }
}

async function loadFolders() {
  try {
    folders.value = await listFolders()
  } catch (err) {
    notifyError(err.message)
  } finally {
    loading.value = false
  }
}

function addFiles(items) {
  const byPath = new Map(selected.value.map((item) => [item.path, item]))
  for (const item of items) byPath.set(item.path, item)
  selected.value = [...byPath.values()]
}

function onPick(event) {
  addFiles(fileItems(event.target.files))
  // Allow picking the same files again after removing them.
  event.target.value = ''
}

function onDragEnter(event) {
  if (!isFileDrag(event)) return
  dragDepth++
  dragging.value = true
}

function onDragLeave(event) {
  if (!isFileDrag(event)) return
  dragDepth = Math.max(0, dragDepth - 1)
  if (!dragDepth) dragging.value = false
}

async function onDrop(event) {
  dragDepth = 0
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

// uploadTo points the upload card at an existing folder.
async function uploadTo(folder) {
  targetId.value = folder.id
  targetPaths.value = new Set()
  uploadCard.value.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
  try {
    const { files } = await fetchFolder(folder.id)
    // Another folder may have been picked meanwhile.
    if (targetId.value === folder.id) targetPaths.value = new Set(files.map((f) => f.path))
  } catch (err) {
    notifyError(err.message)
  }
}

function uploadToNewFolder() {
  targetId.value = null
  targetPaths.value = new Set()
}

async function upload() {
  const items = selected.value
  let folder = target.value
  uploading.value = true
  progress.value = { done: 0, total: items.length }
  try {
    folder ??= await createFolder(ttl.value)
    for (const item of items) {
      await uploadFile(folder.id, item.path, item.file)
      progress.value = { done: progress.value.done + 1, total: items.length }
    }
    selected.value = []
    uploadToNewFolder()
    notifySuccess(items.length === 1 ? 'Uploaded 1 file' : `Uploaded ${items.length} files`)
  } catch (err) {
    notifyError(err.message)
  } finally {
    uploading.value = false
    // A failure part-way still wrote files and spent credits.
    const reloads = [loadFolders(), currentUser.refreshUser()]
    if (folder && expanded.value[folder.id]) reloads.push(loadFiles(folder.id))
    await Promise.all(reloads)
  }
}

// collapse forgets a folder's expanded state.
function collapse(id) {
  const rest = { ...expanded.value }
  delete rest[id]
  expanded.value = rest
}

async function toggleFolder(folder) {
  if (expanded.value[folder.id]) {
    collapse(folder.id)
    return
  }
  expanded.value = { ...expanded.value, [folder.id]: { loading: true, files: [] } }
  await loadFiles(folder.id)
}

async function loadFiles(id) {
  try {
    const { files } = await fetchFolder(id)
    expanded.value = { ...expanded.value, [id]: { loading: false, files } }
  } catch (err) {
    notifyError(err.message)
    collapse(id)
  }
}

async function startRename(folder) {
  editing.value = { id: folder.id, value: folderTitle(folder) }
  await nextTick()
  titleInput.value[0]?.focus()
  titleInput.value[0]?.select()
}

// saveRename stores the edited title. Enter and the blur that follows both
// call it, so the first one ends the edit. Clearing the title restores the
// default one.
async function saveRename(folder) {
  const edit = editing.value
  if (edit?.id !== folder.id) return
  editing.value = null
  const title = edit.value.trim()
  if (title === folderTitle(folder)) return
  try {
    const updated = await renameFolder(folder.id, title)
    folders.value = folders.value.map((f) => (f.id === folder.id ? { ...f, ...updated } : f))
    notifySuccess('Folder renamed')
  } catch (err) {
    notifyError(err.message)
  }
}

async function confirmDelete() {
  const { folder, file } = pendingDelete.value
  deleting.value = true
  try {
    if (file) {
      await deleteFile(folder.id, file.path)
      const entry = expanded.value[folder.id]
      entry.files = entry.files.filter((f) => f.path !== file.path)
      if (targetId.value === folder.id) targetPaths.value.delete(file.path)
      notifySuccess('File deleted')
      // A folder left with one file is named after it.
      await loadFolders()
    } else {
      await deleteFolder(folder.id)
      folders.value = folders.value.filter((f) => f.id !== folder.id)
      if (targetId.value === folder.id) uploadToNewFolder()
      notifySuccess('Folder deleted')
    }
  } catch (err) {
    notifyError(err.message)
  } finally {
    deleting.value = false
    pendingDelete.value = null
  }
}

onMounted(() => {
  loadLimits()
  loadFolders()
  timer = setInterval(() => {
    now.value = Date.now()
  }, TICK_MS)
})

onUnmounted(() => clearInterval(timer))
</script>

<template>
  <div
    class="relative grow flex flex-col"
    data-test="files-page"
    @dragenter="onDragEnter"
    @dragover.prevent
    @dragleave="onDragLeave"
    @drop.prevent="onDrop"
  >
    <div
      v-if="dragging"
      class="pointer-events-none absolute inset-0 z-10 flex items-center justify-center rounded-xl border-2 border-dashed border-gray-300 dark:border-zinc-600 bg-white/90 dark:bg-zinc-900/90"
      data-test="drop-overlay"
    >
      <div class="text-center">
        <svg class="mx-auto w-8 h-8 text-gray-900 dark:text-zinc-100" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
          <path stroke-linecap="round" stroke-linejoin="round" d="M12 16V4m0 0L7 9m5-5 5 5M4 16v3a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-3" />
        </svg>
        <p class="mt-2 text-sm font-bold text-gray-900 dark:text-zinc-100">Drop files or folders</p>
        <p v-if="target" class="text-xs text-gray-500 dark:text-zinc-400">
          They go into {{ folderTitle(target) }}, replacing files with the same path.
        </p>
        <p v-else class="text-xs text-gray-500 dark:text-zinc-400">They go into a new folder that expires after {{ formatMinutes(ttl) }}.</p>
      </div>
    </div>

    <header class="px-4 pb-4 border-b border-gray-100 dark:border-zinc-800">
      <h1 class="text-lg font-bold text-gray-900 dark:text-zinc-100 tracking-tight">Files</h1>
      <p class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">Upload files to a temporary folder and share the links until it expires.</p>
    </header>

    <!-- Upload -->
    <section class="px-4 pt-4">
      <div
        ref="uploadCard"
        class="rounded-xl border border-gray-100 dark:border-zinc-800 hover:border-gray-200 dark:hover:border-zinc-700 p-5 transition-colors"
        data-test="upload-card"
      >
        <div class="flex flex-wrap items-end gap-3">
          <div v-if="target" class="grow min-w-0" data-test="upload-target">
            <h2 class="text-sm font-bold text-gray-900 dark:text-zinc-100">
              Upload to {{ folderTitle(target) }}
            </h2>
            <p class="text-xs text-gray-500 dark:text-zinc-400">
              Files with the same path are replaced and keep their links. The folder keeps its expiry.
            </p>
          </div>
          <div v-else class="grow min-w-0">
            <h2 class="text-sm font-bold text-gray-900 dark:text-zinc-100">New folder</h2>
            <p class="text-xs text-gray-500 dark:text-zinc-400">Choose files or a whole folder, or drop them anywhere on this page.</p>
          </div>
          <label :class="[SECONDARY, 'cursor-pointer']">
            Choose files
            <input type="file" multiple class="sr-only" data-test="pick-files" @change="onPick" />
          </label>
          <label :class="[SECONDARY, 'cursor-pointer']">
            Choose folder
            <input type="file" webkitdirectory multiple class="sr-only" data-test="pick-folder" @change="onPick" />
          </label>
          <div v-if="target" class="w-48 flex items-center justify-between gap-2">
            <div>
              <p class="text-[11px] font-bold text-gray-500 dark:text-zinc-400">Expires in</p>
              <p class="text-xs font-bold tabular-nums text-gray-900 dark:text-zinc-100" data-test="upload-target-expires">
                {{ expiresIn(target.expiresAt, now) }}
              </p>
            </div>
            <button type="button" :disabled="uploading" :class="SECONDARY" data-test="upload-new-folder" @click="uploadToNewFolder">
              New folder
            </button>
          </div>
          <div v-else class="w-48">
            <div class="flex items-baseline justify-between mb-1">
              <label for="upload-ttl" class="text-[11px] font-bold text-gray-500 dark:text-zinc-400">Expires after</label>
              <span class="text-xs font-bold tabular-nums text-gray-900 dark:text-zinc-100" data-test="upload-ttl-value">
                {{ formatMinutes(ttl) }}
              </span>
            </div>
            <input
              id="upload-ttl"
              type="range"
              min="0"
              :max="Math.max(ttlChoices.length - 1, 0)"
              step="1"
              :value="ttlIndex"
              :disabled="!limits"
              :aria-valuetext="formatMinutes(ttl)"
              class="w-full cursor-pointer accent-black dark:accent-white focus:outline-none focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-gray-300 dark:focus-visible:outline-zinc-600 disabled:cursor-not-allowed"
              data-test="upload-ttl"
              @input="ttl = ttlChoices[$event.target.valueAsNumber]"
            />
          </div>
        </div>

        <div v-if="selected.length" class="mt-4 border-t border-gray-100 dark:border-zinc-800 pt-4" data-test="upload-summary">
          <ul class="max-h-48 overflow-y-auto no-scrollbar divide-y divide-gray-50 dark:divide-zinc-800">
            <li
              v-for="item in selected"
              :key="item.path"
              class="flex items-center gap-3 py-1.5 text-xs"
              :data-test="`selected-${item.path}`"
            >
              <span class="grow min-w-0 truncate font-mono text-gray-900 dark:text-zinc-100">{{ item.path }}</span>
              <span
                v-if="targetPaths.has(item.path)"
                class="shrink-0 text-[10px] font-black uppercase tracking-widest text-amber-600 dark:text-amber-400"
                data-test="selected-replaces"
              >
                Replaces
              </span>
              <span
                :class="['tabular-nums', isOversized(item.file) ? 'font-bold text-rose-600 dark:text-rose-400' : 'text-gray-400 dark:text-zinc-500']"
              >
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
            {{ formatBytes(limits.maxUploadBytes) }} limit. Remove {{ oversized.length === 1 ? 'it' : 'them' }} to upload.
          </p>
          <p v-if="targetExpired" role="alert" class="mt-3 text-xs font-medium text-rose-600 dark:text-rose-400" data-test="upload-target-expired">
            {{ folderTitle(target) }} has expired. Upload to a new folder instead.
          </p>
          <p v-if="notEnoughCredits" class="mt-3 rounded-lg border border-amber-200 dark:border-amber-500/30 bg-amber-50 dark:bg-amber-500/10 px-3 py-2 text-xs font-medium text-amber-800 dark:text-amber-300" data-test="upload-credits-warning">
            This upload costs {{ cost }} credits but you have {{ credits }} left.
          </p>

          <div class="mt-4 flex flex-wrap items-center gap-3">
            <p class="grow text-xs text-gray-500 dark:text-zinc-400" data-test="upload-cost">
              {{ selected.length === 1 ? '1 file' : `${selected.length} files` }} ·
              <template v-if="replacing">{{ replacing }} replaced · </template>
              <span class="font-bold text-gray-900 dark:text-zinc-100">{{ cost }} credits</span>
            </p>
            <button type="button" :disabled="uploading" :class="SECONDARY" data-test="upload-clear" @click="selected = []">
              Clear
            </button>
            <button type="button" :disabled="!canUpload" :class="PRIMARY" data-test="upload-start" @click="upload">
              {{ uploading ? `Uploading ${progress.done} / ${progress.total}` : 'Upload' }}
            </button>
          </div>
        </div>
      </div>
    </section>

    <!-- Folders -->
    <section class="px-4 pt-6">
      <h2 class="text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500 mb-2">Folders</h2>

      <div v-if="loading" class="flex justify-center py-12" data-test="folders-loading">
        <svg class="animate-spin h-5 w-5 text-gray-400 dark:text-zinc-500" fill="none" viewBox="0 0 24 24">
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
          <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
        </svg>
      </div>

      <div v-else-if="!folders.length" class="rounded-xl border border-gray-100 dark:border-zinc-800 p-8 text-center" data-test="folders-empty">
        <p class="text-sm font-bold text-gray-900 dark:text-zinc-100">No folders yet</p>
        <p class="mt-1 text-xs text-gray-500 dark:text-zinc-400">Uploaded files show up here until they expire.</p>
      </div>

      <ul v-else class="grid grid-cols-1 items-start gap-2 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
        <li
          v-for="folder in sortedFolders"
          :key="folder.id"
          :class="[
            'min-w-0 rounded-xl border transition-colors',
            targetId === folder.id
              ? 'border-gray-900 dark:border-zinc-400'
              : 'border-gray-100 dark:border-zinc-800 hover:border-gray-200 dark:hover:border-zinc-700',
          ]"
          :data-test="`folder-${folder.id}`"
        >
          <div class="flex items-center gap-1.5 px-3 pt-2">
            <button
              type="button"
              class="inline-flex items-center justify-center size-5 shrink-0 rounded text-gray-400 dark:text-zinc-500 hover:text-gray-900 dark:hover:text-zinc-100"
              :aria-expanded="Boolean(expanded[folder.id])"
              :aria-label="expanded[folder.id] ? 'Hide files' : 'Show files'"
              data-test="folder-toggle"
              @click="toggleFolder(folder)"
            >
              <svg
                :class="['w-3.5 h-3.5 transition-transform', expanded[folder.id] ? 'rotate-90' : '']"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
                stroke-width="2"
              >
                <path stroke-linecap="round" stroke-linejoin="round" d="m9 18 6-6-6-6" />
              </svg>
            </button>
            <input
              v-if="editing?.id === folder.id"
              ref="titleInput"
              v-model="editing.value"
              type="text"
              maxlength="100"
              :placeholder="folder.defaultTitle || folder.id"
              aria-label="Folder title"
              class="grow min-w-0 rounded-md border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-1.5 py-0.5 text-xs font-bold text-gray-900 dark:text-zinc-100 focus:border-gray-400 dark:focus:border-zinc-500 focus:outline-none"
              data-test="folder-title-input"
              @keydown.enter.prevent="saveRename(folder)"
              @keydown.esc="editing = null"
              @blur="saveRename(folder)"
            />
            <button
              v-else
              type="button"
              class="grow min-w-0 truncate rounded-md px-1.5 py-0.5 text-left text-xs font-bold text-gray-900 dark:text-zinc-100 hover:bg-gray-100 dark:hover:bg-zinc-800 cursor-text transition-colors"
              :title="`${folderTitle(folder)} · click to rename`"
              data-test="folder-title"
              @click="startRename(folder)"
            >
              {{ folderTitle(folder) }}
            </button>
          </div>
          <div class="flex items-center gap-1 pl-11 pr-2 pb-1.5">
            <span class="grow min-w-0 truncate text-[11px] text-gray-400 dark:text-zinc-500">
              <template v-if="folderTitle(folder) !== folder.id">
                <span class="font-mono" data-test="folder-id">{{ folder.id }}</span> ·
              </template>
              <span class="tabular-nums" :title="folder.expiresAt" data-test="folder-expires">{{ expiresIn(folder.expiresAt, now) }}</span>
            </span>
            <button
              type="button"
              :class="ICON_BUTTON"
              title="Upload to this folder"
              data-test="folder-upload"
              @click="uploadTo(folder)"
            >
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                <path stroke-linecap="round" stroke-linejoin="round" :d="ICONS.upload" />
              </svg>
            </button>
            <button type="button" :class="ICON_BUTTON" title="Copy folder URL" data-test="folder-copy" @click="copy(folder.url, 'Folder URL copied')">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                <path stroke-linecap="round" stroke-linejoin="round" :d="ICONS.copy" />
              </svg>
            </button>
            <a :href="folder.url" target="_blank" rel="noopener" :class="ICON_BUTTON" title="Open folder" data-test="folder-open">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                <path stroke-linecap="round" stroke-linejoin="round" :d="ICONS.open" />
              </svg>
            </a>
            <button
              type="button"
              :class="[ICON_BUTTON, 'hover:text-rose-600 dark:hover:text-rose-400 hover:bg-rose-50 dark:hover:bg-rose-500/10']"
              title="Delete folder"
              data-test="folder-delete"
              @click="pendingDelete = { folder }"
            >
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                <path stroke-linecap="round" stroke-linejoin="round" :d="ICONS.trash" />
              </svg>
            </button>
          </div>

          <div v-if="expanded[folder.id]" class="border-t border-gray-100 dark:border-zinc-800 px-3 py-2" data-test="folder-files">
            <p v-if="expanded[folder.id].loading" class="py-1 text-xs text-gray-400 dark:text-zinc-500">Loading files…</p>
            <p v-else-if="!expanded[folder.id].files.length" class="py-1 text-xs text-gray-400 dark:text-zinc-500">This folder is empty.</p>
            <ul v-else class="divide-y divide-gray-50 dark:divide-zinc-800">
              <li
                v-for="file in expanded[folder.id].files"
                :key="file.path"
                class="flex items-center gap-2 py-1"
                :data-test="`file-${file.path}`"
              >
                <a
                  :href="file.url"
                  target="_blank"
                  rel="noopener"
                  class="grow min-w-0 truncate font-mono text-xs text-gray-900 dark:text-zinc-100 hover:underline"
                  :title="`Open ${file.path}`"
                  data-test="file-link"
                >{{ file.path }}</a>
                <span class="shrink-0 text-[11px] tabular-nums text-gray-400 dark:text-zinc-500">{{ formatBytes(file.size) }}</span>
                <button type="button" :class="ICON_BUTTON" title="Copy file URL" data-test="file-copy" @click="copy(file.url, 'File URL copied')">
                  <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                    <path stroke-linecap="round" stroke-linejoin="round" :d="ICONS.copy" />
                  </svg>
                </button>
                <button
                  type="button"
                  :class="[ICON_BUTTON, 'hover:text-rose-600 dark:hover:text-rose-400 hover:bg-rose-50 dark:hover:bg-rose-500/10']"
                  title="Delete file"
                  data-test="file-delete"
                  @click="pendingDelete = { folder, file }"
                >
                  <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                    <path stroke-linecap="round" stroke-linejoin="round" :d="ICONS.trash" />
                  </svg>
                </button>
              </li>
            </ul>
          </div>
        </li>
      </ul>
    </section>

    <DeleteModal
      :open="Boolean(pendingDelete)"
      :title="deleteTitle"
      :message="deleteMessage"
      :busy="deleting"
      @confirm="confirmDelete"
      @cancel="pendingDelete = null"
    />
  </div>
</template>
