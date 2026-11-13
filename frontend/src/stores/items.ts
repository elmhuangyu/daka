import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import * as api from '../api'
import type { Category, ItemDetail } from '../api'
import { usePrivacyStore } from './privacy'
import { t } from '../i18n'
import { monthRange, type CategoryFilter } from '../items/logic'

/**
 * Items panel state. Plaintext is dropped synchronously when the privacy guard
 * turns on, and responses of superseded requests are ignored.
 */
export const useItemsStore = defineStore('items', () => {
  const privacy = usePrivacyStore()
  const items = ref<ItemDetail[]>([])
  const categories = ref<Category[]>([])
  const q = ref('')
  const archived = ref(false)
  const filter = ref<CategoryFilter>('all')
  const loading = ref(false)
  const error = ref('')
  let seq = 0

  function clear() {
    seq++
    items.value = []
    categories.value = []
    q.value = ''
    loading.value = false
  }
  watch(
    () => privacy.redact,
    (on) => {
      if (on) clear()
    },
    { flush: 'sync' },
  )

  async function refresh() {
    const my = ++seq
    const redact = privacy.redact
    loading.value = true
    try {
      // Searching a redacted list would be an oracle for hidden names: no q while guarded.
      const [it, cats] = await Promise.all([
        api.listItemsPanel({ q: redact ? '' : q.value.trim(), archived: archived.value, redact }),
        api.getCategories(),
      ])
      if (my !== seq) return
      items.value = (it.items ?? []).filter((item) => item.kind !== 'event')
      categories.value = (cats.categories ?? []).map((c) =>
        redact && c.is_sensitive ? { ...c, name: t('common.privateCategory'), icon: '' } : c,
      )
      error.value = ''
    } catch (e) {
      if (my !== seq) return
      error.value = api.describeError(e)
    } finally {
      if (my === seq) loading.value = false
    }
  }

  async function setPinned(id: number, pinned: boolean) {
    try {
      await api.patchItem(id, { pinned })
      await refresh()
    } catch (e) {
      error.value = api.describeError(e)
    }
  }

  return { items, categories, q, archived, filter, loading, error, clear, refresh, setPinned }
})

/** Detail page state for one item. */
export const useItemDetailStore = defineStore('itemDetail', () => {
  const privacy = usePrivacyStore()
  const id = ref(0)
  const item = ref<api.ItemDto | null>(null)
  const stats = ref<api.ItemStats | null>(null)
  const history = ref<api.HistoryLog[]>([])
  const nextCursor = ref('')
  const month = ref('')
  const calendar = ref<api.ItemCalendarDay[]>([])
  const loading = ref(false)
  const error = ref('')
  const notFound = ref(false)
  let seq = 0
  let calSeq = 0

  function clear() {
    seq++
    calSeq++
    item.value = null
    stats.value = null
    history.value = []
    nextCursor.value = ''
    calendar.value = []
    loading.value = false
  }
  watch(
    () => privacy.redact,
    (on) => {
      if (on) clear()
    },
    { flush: 'sync' },
  )

  async function loadCalendar() {
    const my = ++calSeq
    const redact = privacy.redact
    const { start, end } = monthRange(month.value)
    try {
      const res = await api.getItemCalendar(id.value, start, end, redact)
      if (my === calSeq) calendar.value = res.days ?? []
    } catch (e) {
      if (my === calSeq) error.value = api.describeError(e)
    }
  }

  async function load(itemId: number) {
    if (itemId !== id.value) {
      clear()
      month.value = ''
    }
    const my = ++seq
    id.value = itemId
    const redact = privacy.redact
    loading.value = true
    notFound.value = false
    try {
      const [it, st, h] = await Promise.all([
        api.getItem(itemId, redact),
        api.getItemStats(itemId, redact),
        api.getItemCheckins(itemId, '', redact),
      ])
      if (my !== seq) return
      item.value = it
      stats.value = st
      history.value = h.checkins ?? []
      nextCursor.value = h.next_cursor ?? ''
      if (!month.value) month.value = (st.today ?? '').slice(0, 7)
      error.value = ''
      await loadCalendar()
    } catch (e) {
      if (my !== seq) return
      notFound.value = e instanceof api.ApiError && e.status === 404
      error.value = api.describeError(e)
    } finally {
      if (my === seq) loading.value = false
    }
  }

  async function loadMore() {
    if (!nextCursor.value) return
    const my = seq
    const redact = privacy.redact
    try {
      const h = await api.getItemCheckins(id.value, nextCursor.value, redact)
      if (my !== seq) return
      history.value = [...history.value, ...(h.checkins ?? [])]
      nextCursor.value = h.next_cursor ?? ''
    } catch (e) {
      if (my === seq) error.value = api.describeError(e)
    }
  }

  async function setMonth(ym: string) {
    month.value = ym
    await loadCalendar()
  }

  async function removeCheckin(logId: number) {
    try {
      await api.deleteCheckin(logId)
      await load(id.value)
    } catch (e) {
      error.value = api.describeError(e)
    }
  }

  async function patch(body: Record<string, unknown>) {
    try {
      await api.patchItem(id.value, body)
      await load(id.value)
    } catch (e) {
      error.value = api.describeError(e)
    }
  }

  /** Permanent delete; true on success. */
  async function remove(): Promise<boolean> {
    try {
      await api.deleteItem(id.value)
      clear()
      id.value = 0
      return true
    } catch (e) {
      error.value = api.describeError(e)
      return false
    }
  }

  return {
    id,
    item,
    stats,
    history,
    nextCursor,
    month,
    calendar,
    loading,
    error,
    notFound,
    clear,
    load,
    loadMore,
    setMonth,
    removeCheckin,
    patch,
    remove,
  }
})
