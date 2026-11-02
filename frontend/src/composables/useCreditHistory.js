// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { ref } from 'vue'
import { HISTORY_DAYS } from '../utils/credits'

const DAY_MS = 24 * 60 * 60 * 1000

// useCreditHistory pages through a credit history, newest first.
// fetchPage(before) returns { activities, next }; onError gets what any
// load throws.
export function useCreditHistory(fetchPage, onError) {
  const activities = ref([])
  const next = ref('')
  const loading = ref(true)
  const loadingOlder = ref(false)

  // loadPage appends the next page of history, older than what is shown.
  async function loadPage() {
    const page = await fetchPage(next.value)
    activities.value.push(...page.activities)
    next.value = page.next
    return page.activities
  }

  // loadRange loads every activity of the last HISTORY_DAYS days, plus one
  // before them for the opening balance, which the charts need.
  async function loadRange() {
    const since = (Math.floor(Date.now() / DAY_MS) - HISTORY_DAYS + 1) * DAY_MS
    try {
      let page
      do {
        page = await loadPage()
      } while (next.value && Date.parse(page.at(-1).createdAt) >= since)
    } catch (err) {
      onError(err)
    } finally {
      loading.value = false
    }
  }

  async function loadOlder() {
    loadingOlder.value = true
    try {
      await loadPage()
    } catch (err) {
      onError(err)
    } finally {
      loadingOlder.value = false
    }
  }

  return { activities, next, loading, loadingOlder, loadRange, loadOlder }
}
