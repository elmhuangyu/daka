import { defineStore } from 'pinia'
import { computed, ref, watch } from 'vue'
import * as api from '../api'
import type {
  CalendarDay,
  CalendarViewData,
  Category,
  CheckinInput,
  CheckinLog,
  ExternalEvent,
  ItemDetail,
} from '../api'
import { eventDates, gridRange, shiftAnchor, type CalendarMode } from '../calendar/logic'
import { useOfflineStore } from './offline'
import { usePrivacyStore } from './privacy'
import { t } from '../i18n'

export interface ExternalLayer {
  id: string
  name: string
  status: 'loading' | 'ok' | 'error'
  error: string
  events: ExternalEvent[]
}

/** Stand-in title for external events while the privacy guard is on. */
export const redactedEventTitle = () => t('calendar.extEvent')

/** Today's date in an IANA timezone (falls back to the browser's local date). */
export function todayIn(tz?: string): string {
  try {
    return new Intl.DateTimeFormat('en-CA', {
      timeZone: tz || undefined,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
    }).format(new Date())
  } catch {
    return new Date().toISOString().slice(0, 10)
  }
}

/**
 * State of the Calendar page. Plaintext (item names, logs, external event titles)
 * is held only while the privacy guard is off: turning it on drops it
 * synchronously and invalidates in-flight responses.
 */
export const useCalendarStore = defineStore('calendar', () => {
  const privacy = usePrivacyStore()

  const mode = ref<CalendarMode>('month')
  const anchor = ref(todayIn())
  const weekStart = ref(1)
  const settingsLoaded = ref(false)
  const sources = ref<{ id: string; name: string }[]>([])

  const view = ref<CalendarViewData | null>(null)
  const loading = ref(false)
  const error = ref('')
  const external = ref<ExternalLayer[]>([])

  const selected = ref<string | null>(null)
  const items = ref<ItemDetail[]>([])
  const categories = ref<Category[]>([])
  const dayLogs = ref<CheckinLog[]>([])
  const panelError = ref('')
  const busy = ref<number[]>([])

  let seq = 0 // main view (and external layer) generation
  let panelSeq = 0

  const range = computed(() => gridRange(mode.value, anchor.value, weekStart.value))

  function clearPlaintext() {
    seq++
    panelSeq++
    view.value = null
    items.value = []
    categories.value = []
    dayLogs.value = []
    external.value = external.value.map((l) => ({
      ...l,
      events: [],
      status: l.status === 'ok' ? 'loading' : l.status,
    }))
    loading.value = false
  }

  watch(
    () => privacy.redact,
    (on) => {
      if (on) clearPlaintext()
    },
    { flush: 'sync' },
  )

  async function loadSettings() {
    if (settingsLoaded.value) return
    try {
      const s = await api.getCalendarSettings()
      if (s.week_start === 0 || s.week_start === 1) weekStart.value = s.week_start
      sources.value = (s.external_calendars ?? []).map((c) => ({ id: c.id, name: c.name }))
      if (s.timezone && !selected.value && !view.value) anchor.value = todayIn(s.timezone)
    } catch {
      // Defaults (Monday start, no external layer) keep the calendar usable.
    }
    settingsLoaded.value = true
  }

  async function refresh(withExternal = true) {
    const my = ++seq
    const redact = privacy.redact
    if (!view.value) loading.value = true
    if (!settingsLoaded.value) {
      await loadSettings()
      if (my !== seq) return
    }
    const { start, end } = range.value
    try {
      const v = await api.getCalendarView(start, end, redact)
      if (my !== seq) return
      view.value = { ...v, days: v.days ?? [] }
      error.value = ''
    } catch (e) {
      if (my !== seq) return
      error.value = api.describeError(e)
    } finally {
      if (my === seq) loading.value = false
    }
    if (withExternal && my === seq) void loadExternal(my, start, end, redact)
  }

  /** One independent request per source: a slow or failing source never blocks the others or the grid. */
  async function loadExternal(my: number, start: string, end: string, redact: boolean) {
    external.value = sources.value.map((s) => ({
      id: s.id,
      name: s.name,
      status: 'loading',
      error: '',
      events: [],
    }))
    await Promise.all(
      sources.value.map(async (s) => {
        let next: Partial<ExternalLayer>
        try {
          const res = await api.getExternalCalendar(start, end, s.id)
          const src = (res.sources ?? []).find((x) => x.id === s.id) ?? res.sources?.[0]
          if (!src || !src.ok) next = { status: 'error', error: src?.error || 'fetch_failed' }
          else {
            // Titles are not server-redacted: swap them for a placeholder while the guard is on.
            const events = (src.events ?? []).map((e) =>
              redact ? { ...e, title: redactedEventTitle() } : e,
            )
            next = { status: 'ok', events }
          }
        } catch (e) {
          next = { status: 'error', error: api.describeError(e) }
        }
        if (my !== seq) return
        external.value = external.value.map((l) => (l.id === s.id ? { ...l, ...next } : l))
      }),
    )
  }

  function setMode(m: CalendarMode) {
    if (m === mode.value) return
    mode.value = m
    return refresh()
  }

  function page(n: number) {
    anchor.value = shiftAnchor(mode.value, anchor.value, n)
    return refresh()
  }

  function goToday() {
    anchor.value = view.value?.today ?? todayIn()
    return refresh()
  }

  const dayByDate = computed(
    () => new Map<string, CalendarDay>((view.value?.days ?? []).map((d) => [d.date, d])),
  )

  /** External events per date, flattened over sources that loaded. */
  const externalByDate = computed(() => {
    const out = new Map<string, { source: string; event: ExternalEvent }[]>()
    for (const l of external.value) {
      if (l.status !== 'ok') continue
      for (const e of l.events) {
        for (const d of eventDates(e)) {
          const list = out.get(d) ?? []
          list.push({ source: l.name, event: e })
          out.set(d, list)
        }
      }
    }
    return out
  })

  const selectedDay = computed(() =>
    selected.value ? (dayByDate.value.get(selected.value) ?? null) : null,
  )
  const detailById = computed(() => new Map(items.value.map((d) => [d.id, d])))

  async function loadPanel() {
    const date = selected.value
    if (!date) return
    const my = ++panelSeq
    const redact = privacy.redact
    try {
      const [it, cats, ls] = await Promise.all([
        api.getItems(redact),
        api.getCategories(),
        api.getCheckinsOn(date, redact),
      ])
      if (my !== panelSeq) return
      items.value = it.items ?? []
      categories.value = (cats.categories ?? []).map((c) =>
        redact && c.is_sensitive ? { ...c, name: t('common.privateCategory'), icon: '' } : c,
      )
      dayLogs.value = ls
      panelError.value = ''
    } catch (e) {
      if (my !== panelSeq) return
      panelError.value = api.describeError(e)
    }
  }

  function select(date: string | null) {
    selected.value = date
    panelSeq++
    dayLogs.value = []
    panelError.value = ''
    if (date) return loadPanel()
  }

  async function guarded(id: number, fn: () => Promise<unknown>) {
    if (busy.value.includes(id)) return
    busy.value.push(id)
    try {
      const r = await fn()
      panelError.value = ''
      if ((r as { queued?: boolean } | undefined)?.queued) return // pending, not applied yet
      await Promise.all([refresh(false), loadPanel()])
    } catch (e) {
      panelError.value = api.describeError(e)
    } finally {
      busy.value = busy.value.filter((x) => x !== id)
    }
  }

  /** Retroactive check-in on the selected day. One user action = one Idempotency-Key. */
  function makeUp(input: Omit<CheckinInput, 'date'>) {
    const date = selected.value
    if (!date) return
    const key = api.newIdempotencyKey()
    return guarded(input.item_id, () => useOfflineStore().checkin({ ...input, date }, key))
  }

  function removeLog(itemId: number, logId: number) {
    return guarded(itemId, () => api.deleteCheckin(logId))
  }

  async function updateNote(logId: number, note: string) {
    const existing = dayLogs.value.find((l) => l.id === logId)
    const itemId = existing?.item_id
    if (itemId) {
      return guarded(itemId, async () => {
        const updated = await api.patchCheckin(logId, { note })
        const idx = dayLogs.value.findIndex((l) => l.id === logId)
        if (idx !== -1) {
          dayLogs.value[idx] = { ...dayLogs.value[idx]!, note: updated.note }
        }
      })
    } else {
      try {
        const updated = await api.patchCheckin(logId, { note })
        const idx = dayLogs.value.findIndex((l) => l.id === logId)
        if (idx !== -1) {
          dayLogs.value[idx] = { ...dayLogs.value[idx]!, note: updated.note }
        }
        panelError.value = ''
        await loadPanel()
      } catch (e) {
        panelError.value = api.describeError(e)
      }
    }
  }

  function setDueDate(itemId: number, dueDate: string | null) {
    return guarded(itemId, () => api.setTodoDueDate(itemId, dueDate))
  }

  function setCategory(itemId: number, categoryId: number | null) {
    return guarded(itemId, () => api.patchItem(itemId, { category_id: categoryId }))
  }

  function convertToOngoing(itemId: number, categoryId: number | null) {
    return guarded(itemId, () => api.convertEventToOngoing(itemId, categoryId))
  }

  function deleteItem(itemId: number) {
    return guarded(itemId, () => api.deleteItem(itemId))
  }

  return {
    mode,
    anchor,
    weekStart,
    view,
    loading,
    error,
    external,
    sources,
    selected,
    selectedDay,
    items,
    categories,
    dayLogs,
    panelError,
    busy,
    range,
    dayByDate,
    externalByDate,
    detailById,
    refresh,
    setMode,
    page,
    goToday,
    select,
    loadPanel,
    makeUp,
    removeLog,
    updateNote,
    setDueDate,
    setCategory,
    convertToOngoing,
    deleteItem,
    clearPlaintext,
  }
})
