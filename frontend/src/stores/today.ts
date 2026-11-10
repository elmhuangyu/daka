import { defineStore } from 'pinia'
import { computed, ref, watch } from 'vue'
import * as api from '../api'
import type { Category, CheckinInput, CheckinLog, ItemDetail, TodayView } from '../api'
import { useOfflineStore } from './offline'
import { usePrivacyStore } from './privacy'
import { t } from '../i18n'
import {
  matchesFilter,
  needsAttention,
  ringStats,
  type CardVM,
  type CategoryFilter,
} from '../today/logic'

/**
 * State of the Today page. Holds plaintext only while the privacy guard is off:
 * turning it on clears everything synchronously and drops in-flight responses.
 */
export const useTodayStore = defineStore('today', () => {
  const privacy = usePrivacyStore()

  const view = ref<TodayView | null>(null)
  const items = ref<ItemDetail[]>([])
  const categories = ref<Category[]>([])
  const logs = ref<CheckinLog[]>([])
  const loading = ref(false)
  const error = ref('')
  const filter = ref<CategoryFilter>('all')
  /** Item ids with a write in flight. */
  const busy = ref<number[]>([])

  const selectedDate = ref<string>('')
  const actualToday = ref<string>('')

  const isToday = computed(
    () => !selectedDate.value || !actualToday.value || selectedDate.value === actualToday.value,
  )
  const isFuture = computed(
    () => !!actualToday.value && !!selectedDate.value && selectedDate.value > actualToday.value,
  )

  // Only the latest refresh may update state; stale responses are dropped.
  let seq = 0

  function clear() {
    seq++
    view.value = null
    items.value = []
    categories.value = []
    logs.value = []
    loading.value = false
  }

  watch(
    () => privacy.redact,
    (on) => {
      if (on) clear()
    },
    { flush: 'sync' },
  )

  function offsetDate(dateStr: string, days: number): string {
    const d = new Date(dateStr + 'T00:00:00')
    d.setDate(d.getDate() + days)
    const y = d.getFullYear()
    const m = String(d.getMonth() + 1).padStart(2, '0')
    const day = String(d.getDate()).padStart(2, '0')
    return `${y}-${m}-${day}`
  }

  async function refresh(targetDate?: string) {
    const my = ++seq
    const redact = privacy.redact
    if (targetDate !== undefined) {
      selectedDate.value = targetDate
    }
    if (!view.value) loading.value = true
    try {
      const qDate =
        selectedDate.value && actualToday.value && selectedDate.value !== actualToday.value
          ? selectedDate.value
          : undefined
      const [v, it, cats] = await Promise.all([
        api.getToday(redact, qDate),
        api.getItems(redact),
        api.getCategories(),
      ])
      const ls = await api.getCheckinsOn(v.date, redact)
      if (my !== seq) return
      if (!actualToday.value) {
        actualToday.value = v.date
      }
      view.value = v
      if (qDate) {
        selectedDate.value = v.date
      } else {
        selectedDate.value = ''
      }
      items.value = it.items ?? []
      // The categories endpoint has no redact flag: mask sensitive ones here.
      categories.value = (cats.categories ?? []).map((c) =>
        redact && c.is_sensitive ? { ...c, name: t('common.privateCategory'), icon: '' } : c,
      )
      logs.value = ls
      error.value = ''
      if (typeof filter.value === 'number' && !categories.value.some((c) => c.id === filter.value))
        filter.value = 'all'
    } catch (e) {
      if (my !== seq) return
      error.value = api.describeError(e)
    } finally {
      if (my === seq) loading.value = false
    }
  }

  function goToPrevDay() {
    if (!view.value) return
    return refresh(offsetDate(view.value.date, -1))
  }

  function goToNextDay() {
    if (!view.value) return
    return refresh(offsetDate(view.value.date, 1))
  }

  function goToDate(d: string) {
    if (!d) return
    return refresh(d)
  }

  function goToToday() {
    selectedDate.value = ''
    return refresh('')
  }

  const detailById = computed(() => new Map(items.value.map((d) => [d.id, d])))

  function vm(detail: ItemDetail): CardVM {
    return {
      detail,
      today: view.value?.items.find((t) => t.id === detail.id) ?? null,
      logs: logs.value.filter((l) => l.item_id === detail.id),
    }
  }

  /** Due today (fixed_rrule) cards, joined with their definitions. */
  const dueCards = computed<CardVM[]>(() =>
    (view.value?.items ?? []).flatMap((t) => {
      const d = detailById.value.get(t.id)
      return t.due && d ? [vm(d)] : []
    }),
  )
  const scoped = (cards: CardVM[]) =>
    cards.filter((c) => matchesFilter(c.detail.category_id, filter.value))

  const overdueCards = computed(() =>
    scoped(dueCards.value.filter((c) => c.today?.overdue && !c.today?.done)),
  )
  const todoCards = computed(() =>
    scoped(
      dueCards.value.filter(
        (c) =>
          !c.today?.overdue &&
          (!c.today?.done ||
            c.detail.tracker_type === 'counter' ||
            c.detail.tracker_type === 'metric'),
      ),
    ),
  )
  const unscheduledTodoCards = computed(() => {
    if (isFuture.value) return []
    return scoped(
      items.value
        .filter(
          (d) =>
            d.kind === 'event' &&
            d.current_rule.schedule_type === 'none' &&
            (d.stats?.counts?.total ?? 0) === 0,
        )
        .map(vm)
        .filter((c) => c.logs.length === 0),
    )
  })
  const rollingDone = computed(() =>
    items.value
      .filter((d) => d.current_rule.schedule_type === 'rolling' && !needsAttention(d))
      .map(vm)
      .filter((c) => c.logs.length > 0),
  )
  const unscheduledItems = computed(() =>
    items.value.filter(
      (d) =>
        d.current_rule.schedule_type === 'none' &&
        (d.kind !== 'event' || (d.stats?.counts?.total ?? 0) === 0),
    ),
  )
  const unscheduledDone = computed(() =>
    items.value
      .filter((d) => d.current_rule.schedule_type === 'none')
      .map(vm)
      .filter((c) => c.logs.length > 0),
  )
  const doneCards = computed(() => {
    const map = new Map<number, CardVM>()
    for (const c of dueCards.value.filter(
      (c) =>
        c.today?.done && c.detail.tracker_type !== 'counter' && c.detail.tracker_type !== 'metric',
    )) {
      map.set(c.detail.id, c)
    }
    for (const c of rollingDone.value) {
      map.set(c.detail.id, c)
    }
    for (const c of unscheduledDone.value) {
      map.set(c.detail.id, c)
    }
    return scoped(Array.from(map.values()))
  })
  const maintenanceCards = computed(() => scoped(items.value.filter(needsAttention).map(vm)))
  const quotaCards = computed(() =>
    scoped(items.value.filter((d) => d.current_rule.schedule_type === 'quota').map(vm)),
  )

  const upcoming = computed(() => view.value?.upcoming ?? [])

  /** Ring over the filtered due items; the unfiltered view uses the server's progress. */
  const ring = computed(() => {
    if (!view.value) return { done: 0, total: 0 }
    if (filter.value === 'all') return view.value.progress ?? { done: 0, total: 0 }
    return ringStats(scoped(dueCards.value))
  })

  /** Filter chips: counts cover every visible card of a category. */
  const categoryCounts = computed(() => {
    const rawUnscheduledTodos = isFuture.value
      ? []
      : items.value
          .filter(
            (d) =>
              d.kind === 'event' &&
              d.current_rule.schedule_type === 'none' &&
              (d.stats?.counts?.total ?? 0) === 0,
          )
          .map(vm)
          .filter((c) => c.logs.length === 0)
    const all = [
      ...dueCards.value,
      ...rawUnscheduledTodos,
      ...items.value
        .filter((d) => needsAttention(d) || d.current_rule.schedule_type === 'quota')
        .map(vm),
    ]
    const counts = new Map<number | 'none', number>()
    for (const c of all) {
      const k = c.detail.category_id ?? 'none'
      counts.set(k, (counts.get(k) ?? 0) + 1)
    }
    return { total: all.length, counts }
  })

  async function guarded(id: number, fn: () => Promise<unknown>) {
    if (busy.value.includes(id)) return
    busy.value.push(id)
    try {
      const r = await fn()
      error.value = ''
      // A queued (offline) action is not applied yet: keep the server state, show it as pending.
      if ((r as { queued?: boolean } | undefined)?.queued) return
      await refresh(selectedDate.value)
    } catch (e) {
      error.value = api.describeError(e)
    } finally {
      busy.value = busy.value.filter((x) => x !== id)
    }
  }

  /** One user action = one Idempotency-Key. */
  function submit(input: CheckinInput) {
    const key = api.newIdempotencyKey()
    const payload = { ...input }
    if (!payload.date && !isToday.value && view.value?.date) {
      payload.date = view.value.date
    }
    return guarded(input.item_id, () => useOfflineStore().checkin(payload, key))
  }

  function remove(itemId: number, logId: number) {
    return guarded(itemId, () => api.deleteCheckin(logId))
  }

  async function updateNote(logId: number, note: string) {
    const existing = logs.value.find((l) => l.id === logId)
    const itemId = existing?.item_id
    if (itemId) {
      return guarded(itemId, async () => {
        const updated = await api.patchCheckin(logId, { note })
        const idx = logs.value.findIndex((l) => l.id === logId)
        if (idx !== -1) {
          logs.value[idx] = { ...logs.value[idx]!, note: updated.note }
        }
      })
    } else {
      try {
        const updated = await api.patchCheckin(logId, { note })
        const idx = logs.value.findIndex((l) => l.id === logId)
        if (idx !== -1) {
          logs.value[idx] = { ...logs.value[idx]!, note: updated.note }
        }
        error.value = ''
        await refresh(selectedDate.value)
      } catch (e) {
        error.value = api.describeError(e)
      }
    }
  }

  function setDueDate(itemId: number, dueDate: string | null) {
    return guarded(itemId, () => api.setTodoDueDate(itemId, dueDate))
  }

  return {
    view,
    items,
    categories,
    logs,
    loading,
    error,
    filter,
    busy,
    selectedDate,
    actualToday,
    isToday,
    isFuture,
    overdueCards,
    todoCards,
    unscheduledTodoCards,
    doneCards,
    maintenanceCards,
    quotaCards,
    unscheduledItems,
    upcoming,
    ring,
    categoryCounts,
    refresh,
    clear,
    goToPrevDay,
    goToNextDay,
    goToDate,
    goToToday,
    submit,
    remove,
    updateNote,
    setDueDate,
  }
})
