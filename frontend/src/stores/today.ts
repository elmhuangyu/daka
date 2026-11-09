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

  async function refresh() {
    const my = ++seq
    const redact = privacy.redact
    if (!view.value) loading.value = true
    try {
      const [v, it, cats] = await Promise.all([
        api.getToday(redact),
        api.getItems(redact),
        api.getCategories(),
      ])
      const ls = await api.getCheckinsOn(v.date, redact)
      if (my !== seq) return
      view.value = v
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

  const todoCards = computed(() => scoped(dueCards.value.filter((c) => !c.today?.done)))
  const rollingDone = computed(() =>
    items.value
      .filter((d) => d.current_rule.schedule_type === 'rolling' && !needsAttention(d))
      .map(vm)
      .filter((c) => c.logs.length > 0),
  )
  const doneCards = computed(() =>
    scoped([...dueCards.value.filter((c) => c.today?.done), ...rollingDone.value]),
  )
  const maintenanceCards = computed(() => scoped(items.value.filter(needsAttention).map(vm)))
  const quotaCards = computed(() =>
    scoped(items.value.filter((d) => d.current_rule.schedule_type === 'quota').map(vm)),
  )

  /** Ring over the filtered due items; the unfiltered view uses the server's progress. */
  const ring = computed(() => {
    if (!view.value) return { done: 0, total: 0 }
    if (filter.value === 'all') return view.value.progress
    return ringStats(scoped(dueCards.value))
  })

  /** Filter chips: counts cover every visible card of a category. */
  const categoryCounts = computed(() => {
    const all = [
      ...dueCards.value,
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
      await refresh()
    } catch (e) {
      error.value = api.describeError(e)
    } finally {
      busy.value = busy.value.filter((x) => x !== id)
    }
  }

  /** One user action = one Idempotency-Key. */
  function submit(input: CheckinInput) {
    const key = api.newIdempotencyKey()
    return guarded(input.item_id, () => useOfflineStore().checkin(input, key))
  }

  function remove(itemId: number, logId: number) {
    return guarded(itemId, () => api.deleteCheckin(logId))
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
    todoCards,
    doneCards,
    maintenanceCards,
    quotaCards,
    ring,
    categoryCounts,
    refresh,
    clear,
    submit,
    remove,
  }
})
