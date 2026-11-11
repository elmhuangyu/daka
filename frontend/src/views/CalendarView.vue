<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { Icon } from '@iconify/vue'
import { useRoute, useRouter } from 'vue-router'
import { addDays, periodTitle, weekdayHeaders, type CalendarMode } from '../calendar/logic'
import { useOfflineStore } from '../stores/offline'
import { usePrivacyStore } from '../stores/privacy'
import { useCalendarStore } from '../stores/calendar'
import DayCell from '../components/calendar/DayCell.vue'
import DayPanel from '../components/calendar/DayPanel.vue'
import { catalogDef } from '../composables/shortcutCatalog'
import { useShortcut } from '../composables/useShortcuts'
import { shouldClaimFocus } from '../composables/focusEntry'
import { useI18n } from '../i18n'
import { extCalErrorKey } from '../i18n/errors'

const { t } = useI18n()

const privacy = usePrivacyStore()
const store = useCalendarStore()

const gridEl = ref<HTMLElement | null>(null)
const focusDate = ref(store.anchor)
const panelRef = ref<{ $el: Node } | null>(null)
const calDatePickerInput = ref<HTMLInputElement | null>(null)

async function onCalDatePick(pickedDate: string) {
  if (!pickedDate) return
  store.anchor = pickedDate
  await store.refresh()
  focusDate.value = pickedDate
  void store.select(pickedDate)
}

const todayStr = computed(() => store.view?.today ?? '')
const headers = computed(() => weekdayHeaders(store.weekStart))
const title = computed(() => periodTitle(store.mode, store.anchor, store.weekStart))
const rows = computed(() => {
  const d = store.range.dates
  return Array.from({ length: d.length / 7 }, (_, i) => d.slice(i * 7, i * 7 + 7))
})
const monthPrefix = computed(() => store.anchor.slice(0, 7))
const failedSources = computed(() => store.external.filter((l) => l.status === 'error'))
const loadingSources = computed(() => store.external.filter((l) => l.status === 'loading'))

function inRange(date: string) {
  return date >= store.range.start && date <= store.range.end
}

async function focusCell(date: string) {
  await nextTick()
  gridEl.value?.querySelector<HTMLElement>(`[data-date="${date}"]`)?.focus()
}

/** Page the period. Only keyboard-triggered paging moves focus; header buttons keep theirs. */
async function navigate(n: number, focus = false) {
  await store.page(n)
  if (focus) await focusCell(focusDate.value)
}

// Keep the roving tabindex valid whenever the visible range changes (paging, Today, mode
// switch), without moving focus.
watch(
  () => [store.range.start, store.range.end],
  () => {
    if (inRange(focusDate.value)) return
    const today = store.view?.today ?? ''
    focusDate.value =
      today && inRange(today)
        ? today
        : inRange(store.anchor)
          ? store.anchor
          : (store.range.dates[0] ?? store.anchor)
  },
)

async function goToday() {
  await store.goToday()
  focusDate.value = store.view?.today ?? store.anchor
  await focusCell(focusDate.value)
}

async function moveFocus(target: string) {
  focusDate.value = target
  if (!inRange(target)) {
    store.anchor = target
    await store.refresh()
  }
  await focusCell(target)
}

function panelRows(): HTMLElement[] {
  const root = panelRef.value?.$el as Element | undefined
  return Array.from(root?.querySelectorAll?.<HTMLElement>('[data-panel-item]') ?? [])
}
const inPanel = () =>
  !!(panelRef.value?.$el as Element | undefined)?.contains?.(document.activeElement)

/** Select the focused day if needed and focus its DayPanel item list (or quick-add input). */
async function enterPanel(last = false): Promise<boolean> {
  if (store.selected !== focusDate.value) {
    await store.select(focusDate.value)
  }
  await nextTick()
  const rows = panelRows()
  const target = last ? rows[rows.length - 1] : rows[0]
  if (target) {
    target.focus()
    return true
  }
  const root = panelRef.value?.$el as Element | undefined
  const fallback =
    root?.querySelector?.<HTMLElement>('[data-testid="panel-quick-input"]') ??
    root?.querySelector?.<HTMLElement>('[data-testid="panel-close"]')
  if (!fallback) return false
  fallback.focus()
  return true
}

/** Esc inside the item list: back to the selected day's cell, selection kept. */
function leavePanel() {
  const d = store.selected
  if (!d) return
  focusDate.value = d
  void focusCell(d)
}

function onGridKey(e: KeyboardEvent) {
  if (e.altKey || e.ctrlKey || e.metaKey) return
  const step: Record<string, number> = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 }
  if (e.key in step) {
    e.preventDefault()
    void moveFocus(addDays(focusDate.value, step[e.key]!))
  } else if (e.key === 'Enter') {
    e.preventDefault()
    void enterPanel(false)
  } else if (e.key === 'PageUp' || e.key === 'PageDown') {
    e.preventDefault()
    void navigate(e.key === 'PageUp' ? -1 : 1, true)
  } else if (e.key === 'Escape' && store.selected) {
    const d = store.selected
    void store.select(null)
    void focusCell(d)
  }
}

/** Shortcut entry: act only once the grid exists and focus is not already inside it. */
function fromOutside(fn: () => void) {
  return () => {
    // false = not handled, so the key keeps its default (page scroll) while the grid loads.
    if (!store.view || gridEl.value?.contains(document.activeElement) || inPanel()) return false
    fn()
  }
}
const ARROW_STEPS = [
  ['Left', -1],
  ['Right', 1],
  ['Up', -7],
  ['Down', 7],
] as const
useShortcut([
  ...ARROW_STEPS.map(([name, step]) =>
    catalogDef(
      `calendar.move${name}`,
      fromOutside(() => {
        void moveFocus(addDays(focusDate.value, step))
      }),
    ),
  ),
  catalogDef(
    'calendar.pageUp',
    fromOutside(() => void navigate(-1, true)),
  ),
  catalogDef(
    'calendar.pageDown',
    fromOutside(() => void navigate(1, true)),
  ),
  catalogDef('calendar.select'),
  catalogDef('calendar.month', () => setMode('month')),
  catalogDef('calendar.week', () => setMode('week')),
  catalogDef('calendar.today', () => void goToday()),
  catalogDef('calendar.prev', () => void navigate(-1, true)),
  catalogDef('calendar.next', () => void navigate(1, true)),
])

function select(date: string) {
  focusDate.value = date
  void store.select(store.selected === date ? null : date)
}

function setMode(m: CalendarMode) {
  void store.setMode(m)
}

// URL state: `?date=YYYY-MM-DD` is the selected day (or the shown period when none is
// selected) and `?mode=week` switches to week view. No parameters means today, month view.
const route = useRoute()
const router = useRouter()
const DATE_RE = /^\d{4}-\d{2}-\d{2}$/
function queryOf() {
  const one = (v: unknown) => (Array.isArray(v) ? v[0] : v)
  const d = one(route.query.date)
  const m = one(route.query.mode)
  return {
    date: typeof d === 'string' && DATE_RE.test(d) ? d : '',
    mode: (m === 'week' ? 'week' : 'month') as CalendarMode,
  }
}
let synced = false
const wanted = computed(() => {
  const today = store.view?.today ?? ''
  return {
    date: store.selected ?? (store.anchor !== today ? store.anchor : ''),
    mode: store.mode,
  }
})
async function applyQuery() {
  synced = false // URL -> store: do not echo the intermediate state back into the URL
  const q = queryOf()
  store.mode = q.mode
  store.anchor = q.date || store.view?.today || store.anchor
  await store.refresh()
  store.anchor = q.date || store.view?.today || store.anchor
  focusDate.value = q.date || store.view?.today || store.anchor
  await store.select(q.date || null)
  synced = true
  // Give keyboard users an entry point, but never steal focus the user already placed.
  await nextTick()
  if (shouldClaimFocus()) await focusCell(focusDate.value)
}
watch(wanted, (w) => {
  if (!synced || route.name !== 'calendar') return
  const q = queryOf()
  if (w.date === q.date && w.mode === q.mode) return
  const query: Record<string, string> = {}
  if (w.date) query.date = w.date
  if (w.mode === 'week') query.mode = 'week'
  void router.push({ query })
})
// Back/forward and edited URLs.
watch(
  () => route.query,
  () => {
    if (!synced || route.name !== 'calendar') return
    const q = queryOf()
    if (q.date === wanted.value.date && q.mode === wanted.value.mode) return
    void applyQuery()
  },
)

onMounted(() => {
  // The URL is the source of truth: no parameters means today (nothing selected), month view.
  void applyQuery()
})
// The store drops all plaintext synchronously when redact turns on; refetch afterwards.
watch(
  () => useOfflineStore().syncedTick,
  () => {
    void store.refresh()
    void store.loadPanel()
  },
)
watch(
  () => privacy.redact,
  () => {
    void store.refresh()
    void store.loadPanel()
  },
)
</script>

<template>
  <section class="mx-auto max-w-6xl space-y-4 p-4">
    <header class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h1 class="text-2xl font-bold tracking-tight">{{ t('nav.calendar') }}</h1>
        <h2
          class="text-base font-semibold text-primary mt-0.5"
          aria-live="polite"
          data-testid="period-title"
        >
          {{ title }}
        </h2>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <div class="join shadow-xs" role="group" :aria-label="t('calendar.view')">
          <button
            v-for="m in ['month', 'week'] as const"
            :key="m"
            type="button"
            class="btn btn-sm join-item"
            :class="store.mode === m ? 'btn-primary' : ''"
            :aria-pressed="store.mode === m"
            :data-testid="`mode-${m}`"
            @click="setMode(m)"
          >
            {{ t(m === 'month' ? 'calendar.month' : 'calendar.week') }}
          </button>
        </div>
        <button
          type="button"
          class="btn btn-sm shadow-xs"
          data-testid="go-today"
          @click="store.goToday()"
        >
          {{ t('nav.today') }}
        </button>
        <div class="relative flex items-center">
          <input
            ref="calDatePickerInput"
            type="date"
            class="sr-only"
            :value="store.anchor"
            data-testid="calendar-date-picker"
            @change="onCalDatePick(($event.target as HTMLInputElement).value)"
          />
          <button
            type="button"
            class="btn btn-sm shadow-xs gap-1.5"
            :title="t('calendar.pickDate')"
            data-testid="open-calendar-date-picker"
            @click="
              calDatePickerInput?.showPicker
                ? calDatePickerInput.showPicker()
                : calDatePickerInput?.focus()
            "
          >
            <Icon icon="lucide:calendar" class="size-4" />
            <span class="hidden sm:inline">{{ t('calendar.pickDate') }}</span>
          </button>
        </div>
        <div class="join shadow-xs">
          <button
            type="button"
            class="btn btn-sm btn-square join-item"
            :aria-label="t(store.mode === 'month' ? 'calendar.prev.month' : 'calendar.prev.week')"
            data-testid="prev"
            @click="navigate(-1)"
          >
            <Icon icon="lucide:chevron-left" aria-hidden="true" />
          </button>
          <button
            type="button"
            class="btn btn-sm btn-square join-item"
            :aria-label="t(store.mode === 'month' ? 'calendar.next.month' : 'calendar.next.week')"
            data-testid="next"
            @click="navigate(1)"
          >
            <Icon icon="lucide:chevron-right" aria-hidden="true" />
          </button>
        </div>
      </div>
    </header>

    <p
      v-if="store.loading && !store.view"
      role="status"
      class="py-8 text-center opacity-70"
      data-testid="loading"
    >
      <span class="loading loading-spinner loading-sm" aria-hidden="true"></span>
      {{ t('common.loading') }}
    </p>

    <div v-if="store.error" class="alert alert-error" role="alert" data-testid="error">
      <span>{{ store.error }}</span>
      <button type="button" class="btn btn-sm" data-testid="retry" @click="store.refresh()">
        {{ t('common.retry') }}
      </button>
    </div>

    <div
      v-if="store.view && !store.view.holiday_published"
      class="alert alert-info py-2 px-3 text-xs flex items-center gap-2 shadow-xs"
      data-testid="holiday-unpublished"
    >
      <Icon icon="lucide:info" class="size-4 shrink-0" aria-hidden="true" />
      <span>{{ t('calendar.holidayUnpublished') }}</span>
    </div>

    <div
      v-if="failedSources.length || loadingSources.length"
      class="space-y-1 text-sm"
      data-testid="external-status"
    >
      <p
        v-for="l in failedSources"
        :key="l.id"
        class="text-warning flex items-center gap-1.5"
        role="status"
        :data-testid="`ext-error-${l.id}`"
      >
        <Icon icon="lucide:triangle-alert" aria-hidden="true" class="size-4 shrink-0" />
        {{ t('calendar.extFailed', { name: l.name, reason: t(extCalErrorKey(l.error ?? '')) }) }}
      </p>
      <p
        v-if="loadingSources.length"
        class="opacity-70 flex items-center gap-1.5"
        role="status"
        data-testid="ext-loading"
      >
        <span class="loading loading-spinner loading-xs" aria-hidden="true"></span>
        {{ t('calendar.extLoading') }}
      </p>
    </div>

    <div class="grid gap-4 lg:grid-cols-[1fr_22rem]">
      <div v-if="store.view" class="min-w-0" :class="store.loading ? 'opacity-70' : ''">
        <div class="card card-border bg-base-100 border-base-300 shadow-sm overflow-hidden">
          <div
            ref="gridEl"
            role="grid"
            :aria-label="t('calendar.gridLabel', { title })"
            :aria-busy="store.loading"
            data-testid="grid"
            @keydown="onGridKey"
          >
            <div role="row" class="grid grid-cols-7 border-b border-base-300 bg-base-200/50">
              <div
                v-for="h in headers"
                :key="h.long"
                role="columnheader"
                class="py-2 text-center text-xs font-semibold text-base-content/70 uppercase tracking-wider"
              >
                <abbr :title="h.long" class="no-underline">{{ h.short }}</abbr>
              </div>
            </div>
            <div
              v-for="(r, ri) in rows"
              :key="ri"
              role="row"
              class="grid grid-cols-7 border-b border-base-300 last:border-b-0"
            >
              <DayCell
                v-for="d in r"
                :key="d"
                :date="d"
                :day="store.dayByDate.get(d) ?? null"
                :today="todayStr"
                :in-month="store.mode === 'week' || d.startsWith(monthPrefix)"
                :selected="store.selected === d"
                :focusable="focusDate === d"
                :expanded="store.mode === 'week'"
                :external="store.externalByDate.get(d) ?? []"
                @select="select"
              />
            </div>
          </div>
        </div>
        <div class="card card-border bg-base-100 border-base-300 p-3 shadow-xs mt-3">
          <ul
            class="flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs text-base-content/80"
            :aria-label="t('calendar.legend.label')"
          >
            <li class="flex items-center gap-1.5">
              <span class="inline-block size-2 rounded-full bg-success"></span>
              {{ t('calendar.legend.done') }}
            </li>
            <li class="flex items-center gap-1.5">
              <span class="inline-block size-2 rounded-full bg-warning"></span>
              {{ t('calendar.legend.partial') }}
            </li>
            <li class="flex items-center gap-1.5">
              <span class="inline-block size-2 rounded-full bg-base-content/40"></span>
              {{ t('calendar.legend.missed') }}
            </li>
            <li class="flex items-center gap-1.5">
              <span class="inline-block size-2 rounded-full bg-primary"></span>
              {{ t('calendar.legend.open') }}
            </li>
            <li class="flex items-center gap-1.5">
              <span class="inline-block size-2 rounded-full border border-base-content/50"></span>
              {{ t('calendar.legend.forecast') }}
            </li>
            <li class="flex items-center gap-1.5">
              <span class="inline-block size-2 rounded-full bg-error"></span>
              {{ t('calendar.legend.overdue') }}
            </li>
            <li class="w-full text-base-content/50 pt-1 border-t border-base-300/60 mt-0.5">
              {{ t(store.mode === 'month' ? 'calendar.hint.month' : 'calendar.hint.week') }}
            </li>
          </ul>
        </div>
      </div>
      <DayPanel
        v-if="store.selected"
        ref="panelRef"
        @close="select(store.selected!)"
        @back="leavePanel"
      />
    </div>
  </section>
</template>
