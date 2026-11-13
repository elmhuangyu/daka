<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { Icon } from '@iconify/vue'
import { useOfflineStore } from '../stores/offline'
import { usePrivacyStore } from '../stores/privacy'
import { useProjectsStore } from '../stores/projects'
import { useTodayStore } from '../stores/today'
import { findCategory, groupByTree, type CardVM } from '../today/logic'
import { celebrate, celebrateAll, trackPointer } from '../composables/useCelebration'
import CompletionRing from '../components/today/CompletionRing.vue'
import CategoryFilter from '../components/today/CategoryFilter.vue'
import ItemCard from '../components/today/ItemCard.vue'
import OccurrenceDetailModal from '../components/today/OccurrenceDetailModal.vue'
import {
  createAdHocEvent,
  createScheduledEvent,
  createTodoItem,
  moveItem,
  describeError,
  type CheckinInput,
} from '../api'
import { catalogDef } from '../composables/shortcutCatalog'
import { useShortcut } from '../composables/useShortcuts'
import { useI18n } from '../i18n'
import { formatCustomDate, weekdayName } from '../i18n/format'

const { t } = useI18n()

const privacy = usePrivacyStore()
const store = useTodayStore()
const offline = useOfflineStore()
const projectsStore = useProjectsStore()

const datePickerInput = ref<HTMLInputElement | null>(null)
const upcomingDays = ref<3 | 7 | 14>(14)

const filteredUpcoming = computed(() =>
  store.upcoming.filter((u) => u.days_until <= upcomingDays.value),
)

const quickLogQuery = ref('')
const quickTodoDate = ref('')
const quickTodoProjectId = ref<number | null>(null)
const quickLogOpen = ref(false)
const quickLogIndex = ref(-1)
const quickLogBusy = ref(false)
const dateDropdownOpen = ref(false)
const dateChipRef = ref<HTMLElement | null>(null)
const projectDropdownOpen = ref(false)
const projectChipRef = ref<HTMLElement | null>(null)

const selectedProject = computed(() =>
  quickTodoProjectId.value !== null
    ? (projectsStore.projectById.get(quickTodoProjectId.value) ?? null)
    : null,
)

function setProjectOption(id: number | null) {
  quickTodoProjectId.value = id
  projectDropdownOpen.value = false
}

function resetQuickTodoDate() {
  if (store.isToday || store.isFuture) {
    quickTodoDate.value = store.view?.date ?? ''
  } else {
    quickTodoDate.value = ''
  }
}

watch(
  [() => store.view?.date, () => store.isToday, () => store.isFuture],
  () => {
    resetQuickTodoDate()
  },
  { immediate: true },
)

function offsetDate(dateStr: string, days: number): string {
  if (!dateStr) return ''
  const d = new Date(dateStr + 'T00:00:00')
  d.setDate(d.getDate() + days)
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

const todayDate = computed(() => store.actualToday || store.view?.date || '')
const tomorrowDate = computed(() => offsetDate(todayDate.value, 1))

const dateChipLabel = computed(() => {
  if (!quickTodoDate.value) return ''
  if (todayDate.value && quickTodoDate.value === todayDate.value) {
    return t('today.dateOption.today')
  }
  if (tomorrowDate.value && quickTodoDate.value === tomorrowDate.value) {
    return t('today.tomorrow')
  }
  return formatCustomDate(quickTodoDate.value)
})

function setDateOption(val: string) {
  quickTodoDate.value = val
  dateDropdownOpen.value = false
}

function onDocClick(e: MouseEvent) {
  if (
    dateDropdownOpen.value &&
    dateChipRef.value &&
    !dateChipRef.value.contains(e.target as Node)
  ) {
    dateDropdownOpen.value = false
  }
  if (
    projectDropdownOpen.value &&
    projectChipRef.value &&
    !projectChipRef.value.contains(e.target as Node)
  ) {
    projectDropdownOpen.value = false
  }
}
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    if (dateDropdownOpen.value) dateDropdownOpen.value = false
    if (projectDropdownOpen.value) projectDropdownOpen.value = false
  }
}

const matchingUnscheduled = computed(() => {
  const q = quickLogQuery.value.trim().toLowerCase()
  if (!q) return store.unscheduledItems
  return store.unscheduledItems.filter((it) => it.name.toLowerCase().includes(q))
})

const pinnedUnscheduledOngoing = computed(() =>
  store.unscheduledItems.filter((it) => it.kind !== 'event' && it.pinned),
)

function todayCount(it: { id: number; tracker_type?: string }): number | null {
  const itemLogs = store.logs.filter((l) => l.item_id === it.id)
  if (!itemLogs.length) return null
  if (it.tracker_type === 'counter') {
    return itemLogs.reduce((acc, l) => acc + (l.value ?? 0), 0)
  }
  return itemLogs.length
}

watch(quickLogQuery, () => {
  quickLogIndex.value = -1
})

const quickLogOptions = computed(() => {
  const opts: Array<{ key: string; action: () => void }> = []
  const q = quickLogQuery.value.trim()
  if (q) {
    opts.push({
      key: 'create-todo',
      action: () =>
        void submitQuickTodo({
          name: quickLogQuery.value,
          dueDate: quickTodoDate.value || (store.isFuture ? (store.view?.date ?? null) : null),
        }),
    })
    if (
      store.isFuture ||
      !matchingUnscheduled.value.some((it) => it.name.toLowerCase() === q.toLowerCase())
    ) {
      opts.push({
        key: 'create-log',
        action: () => void submitQuickLog({ name: quickLogQuery.value }),
      })
    }
  }
  if (!store.isFuture) {
    for (const u of matchingUnscheduled.value) {
      opts.push({
        key: `item-${u.id}`,
        action: () => void submitQuickLog(u),
      })
    }
  }
  return opts
})

function onBlurQuickLog() {
  setTimeout(() => {
    quickLogOpen.value = false
    quickLogIndex.value = -1
  }, 200)
}

function onQuickLogKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    quickLogOpen.value = false
    quickLogIndex.value = -1
    return
  }
  if ((e.key === 'ArrowDown' || e.key === 'ArrowUp') && quickLogQuery.value.trim()) {
    const len = quickLogOptions.value.length
    if (!len) return
    e.preventDefault()
    quickLogOpen.value = true
    if (e.key === 'ArrowDown') {
      quickLogIndex.value = quickLogIndex.value < len - 1 ? quickLogIndex.value + 1 : 0
    } else {
      quickLogIndex.value = quickLogIndex.value > 0 ? quickLogIndex.value - 1 : len - 1
    }
    return
  }
  if (e.key === 'Enter') {
    e.preventDefault()
    const picked = quickLogIndex.value >= 0 ? quickLogOptions.value[quickLogIndex.value] : undefined
    if (picked) {
      picked.action()
    } else {
      onEnterQuickAdd()
    }
  }
}

function onEnterQuickAdd() {
  submitQuickTodo({
    name: quickLogQuery.value,
    dueDate: quickTodoDate.value || (store.isFuture ? (store.view?.date ?? null) : null),
  })
}

async function submitQuickTodo(target: { name: string; dueDate?: string | null }) {
  if (quickLogBusy.value) return
  const name = target.name.trim()
  if (!name) return
  quickLogBusy.value = true
  try {
    const due = target.dueDate?.trim() || null
    await createTodoItem(name, due, quickTodoProjectId.value)
    quickLogQuery.value = ''
    resetQuickTodoDate()
    quickTodoProjectId.value = null
    quickLogOpen.value = false
    await store.refresh(store.view?.date)
  } catch (e) {
    store.error = describeError(e)
  } finally {
    quickLogBusy.value = false
  }
}

async function submitQuickLog(target: { id?: number; name: string }) {
  if (quickLogBusy.value) return
  const targetDate = store.view?.date
  if (!targetDate) return
  const name = target.name.trim()
  if (!name && !target.id) return
  quickLogBusy.value = true
  try {
    if (store.isFuture) {
      await createScheduledEvent(name, targetDate)
    } else {
      let itemId = target.id
      let existingItem = target.id ? store.items.find((it) => it.id === target.id) : undefined
      if (!itemId) {
        const exact = matchingUnscheduled.value.find(
          (it) => it.name.toLowerCase() === name.toLowerCase(),
        )
        if (exact) {
          itemId = exact.id
          existingItem = exact
        }
      }
      if (itemId) {
        if (existingItem?.tracker_type === 'counter') {
          const step = existingItem.step && existingItem.step > 0 ? existingItem.step : 1
          await store.submit({ item_id: itemId, mode: 'delta', value: step })
        } else if (existingItem?.tracker_type === 'metric') {
          const step = existingItem.step && existingItem.step > 0 ? existingItem.step : 1
          await store.submit({ item_id: itemId, mode: 'append', value: step })
        } else {
          await store.submit({ item_id: itemId, mode: 'set', value: 1 })
        }
      } else {
        const created = await createAdHocEvent(name)
        await store.submit({ item_id: created.id, mode: 'set', value: 1 })
      }
    }
    quickLogQuery.value = ''
    resetQuickTodoDate()
    quickLogOpen.value = false
    await store.refresh(targetDate)
  } catch (e) {
    store.error = describeError(e)
  } finally {
    quickLogBusy.value = false
  }
}

/** Groups cards by project and nests sub-tasks under their ancestors (shared with Calendar). */
function grouped(cards: CardVM[]) {
  return groupByTree(
    cards,
    (c) => ({ id: c.detail.id, projectId: c.detail.project_id ?? null }),
    (id) => store.detailById.get(id)?.parent_id ?? null,
  )
}
const indent = (c: object) =>
  'depth' in c && typeof c.depth === 'number' && c.depth > 0
    ? { marginLeft: `${Math.min(c.depth, 6) * 1.25}rem` }
    : {}

const sections = computed(
  () =>
    [
      {
        id: 'todo',
        title: t('today.section.todo'),
        icon: 'lucide:list-checks',
        variant: 'todo',
        cards: grouped(store.todoCards.filter((c) => c.detail.kind === 'event')),
      },
      {
        id: 'ongoing',
        title: t('today.section.ongoing'),
        icon: 'lucide:repeat',
        variant: 'todo',
        cards: grouped([
          ...store.todoCards.filter((c) => c.detail.kind !== 'event'),
          ...store.doneCards.filter((c) => c.detail.kind !== 'event'),
        ]),
      },
      {
        id: 'maintenance',
        title: t('today.section.maintenance'),
        icon: 'lucide:repeat',
        variant: 'maintenance',
        cards: store.maintenanceCards,
      },
    ] as const,
)
const visibleCount = computed(
  () =>
    store.overdueCards.length +
    store.todoCards.length +
    store.unscheduledTodoCards.length +
    store.maintenanceCards.length +
    store.goalCards.length +
    store.quotaCards.length +
    store.doneCards.length,
)

function cat(c: CardVM) {
  return findCategory(store.categories, c.detail.category_id)
}
const poolCards = computed(() =>
  grouped([
    ...store.unscheduledTodoCards,
    ...store.doneCards.filter((c) => c.detail.kind === 'event'),
  ]),
)
const overdueGrouped = computed(() => grouped(store.overdueCards))
const doneIds = computed(() => new Set(store.doneCards.map((c) => c.detail.id)))

/** Completed one-shot items stay in their section, dimmed. */
function isDoneCard(c: CardVM) {
  if (doneIds.value.has(c.detail.id)) return true
  return (
    !!c.today?.done && c.detail.tracker_type !== 'counter' && c.detail.tracker_type !== 'metric'
  )
}
function projectFor(c: CardVM) {
  const pid = c.detail.project_id
  if (!pid) return null
  return projectsStore.projectById.get(pid) ?? null
}
trackPointer()

// Drag-to-reorder among siblings (same parent) via pointer events (mouse + touch).
const drag = ref<{ id: number; beforeId: number | null; afterId: number | null } | null>(null)
function clearDropMarks() {
  document.querySelectorAll('.drop-before, .drop-after').forEach((el) => {
    el.classList.remove('drop-before', 'drop-after')
  })
}
function onDragStart(e: PointerEvent, c: CardVM) {
  if (e.pointerType === 'mouse' && e.button !== 0) return
  const handle = e.currentTarget as HTMLElement
  const li = handle.closest('li') as HTMLElement | null
  const ul = li?.parentElement
  if (!li || !ul) return
  e.preventDefault()
  const id = c.detail.id
  const parent = String(c.detail.parent_id ?? '')
  const project = String(c.detail.project_id ?? '')
  const sibs = [...ul.children].filter(
    (x) =>
      x !== li &&
      (x as HTMLElement).dataset.parent === parent &&
      (x as HTMLElement).dataset.project === project,
  ) as HTMLElement[]
  if (!sibs.length) return
  handle.setPointerCapture(e.pointerId)
  drag.value = { id, beforeId: null, afterId: null }
  li.classList.add('opacity-50')
  const move = (ev: PointerEvent) => {
    clearDropMarks()
    const y = ev.clientY
    let target: HTMLElement | null = null
    let after = false
    for (const s of sibs) {
      const r = s.getBoundingClientRect()
      if (y < r.top + r.height / 2) {
        target = s
        after = false
        break
      }
      target = s
      after = true
    }
    if (!target || !drag.value) return
    target.classList.add(after ? 'drop-after' : 'drop-before')
    const tid = Number(target.dataset.id)
    drag.value.beforeId = after ? null : tid
    drag.value.afterId = after ? tid : null
  }
  const end = async (ev: PointerEvent) => {
    handle.removeEventListener('pointermove', move)
    handle.removeEventListener('pointerup', end)
    handle.removeEventListener('pointercancel', end)
    if (handle.hasPointerCapture(ev.pointerId)) handle.releasePointerCapture(ev.pointerId)
    clearDropMarks()
    li.classList.remove('opacity-50')
    const d = drag.value
    drag.value = null
    if (ev.type === 'pointercancel' || !d || (d.beforeId === null && d.afterId === null)) return
    try {
      await moveItem(
        d.id,
        d.beforeId !== null ? { before_id: d.beforeId } : { after_id: d.afterId as number },
      )
      await store.refresh(store.view?.date)
    } catch (err) {
      store.error = describeError(err)
    }
  }
  handle.addEventListener('pointermove', move)
  handle.addEventListener('pointerup', end)
  handle.addEventListener('pointercancel', end)
}
const doneTotal = () => store.ring.done + store.todoRing.done + store.doneCards.length
const allFinished = () =>
  store.ring.total + store.todoRing.total > 0 &&
  store.ring.done + store.todoRing.done >= store.ring.total + store.todoRing.total
const moodPct = computed(() => {
  const total = store.ring.total + store.todoRing.total
  return total ? Math.round(((store.ring.done + store.todoRing.done) / total) * 100) : 0
})
const mood = computed(() =>
  moodPct.value >= 100 ? 'full' : moodPct.value >= 50 ? 'mid' : moodPct.value > 0 ? 'low' : 'idle',
)
async function log(input: CheckinInput) {
  const before = doneTotal()
  const wasFinished = allFinished()
  await store.submit(input)
  if (input.mode === 'unset' || doneTotal() <= before) return
  if (!wasFinished && allFinished()) celebrateAll()
  else celebrate()
}

const detailItemId = ref<number | null>(null)
const allCards = computed<CardVM[]>(() => [
  ...store.overdueCards,
  ...store.todoCards,
  ...store.unscheduledTodoCards,
  ...store.maintenanceCards,
  ...store.goalCards,
  ...store.quotaCards,
  ...store.doneCards,
])
const detailCard = computed(() =>
  detailItemId.value !== null
    ? (allCards.value.find((c) => c.detail.id === detailItemId.value) ?? null)
    : null,
)

// The viewed day lives in the URL (`?date=YYYY-MM-DD`); no parameter means today.
const route = useRoute()
const router = useRouter()
const DATE_RE = /^\d{4}-\d{2}-\d{2}$/
function queryDate(): string {
  const q = route.query.date
  const v = Array.isArray(q) ? q[0] : q
  return typeof v === 'string' && DATE_RE.test(v) ? v : ''
}
watch(
  () => store.selectedDate,
  (d) => {
    if (route.name !== 'today' || d === queryDate()) return
    const { date: _drop, ...rest } = route.query
    const to = { query: d ? { ...rest, date: d } : rest }
    // Normalising ?date=<today> to no parameter must not add a history entry.
    if (!d && queryDate() === store.actualToday) void router.replace(to)
    else void router.push(to)
  },
)
// Back/forward and edited URLs.
watch(
  () => route.query.date,
  () => {
    if (route.name !== 'today') return
    const q = queryDate()
    if (q === store.selectedDate || (q === store.actualToday && !store.selectedDate)) return
    void store.refresh(q)
  },
)

/** Click the enabled toggle of the focused card; a no-op without a focused card or toggle. */
function toggleFocusedCard() {
  const card = document.activeElement?.closest('[data-card]')
  card?.querySelector<HTMLButtonElement>('[data-testid="toggle"]:not(:disabled)')?.click()
}

function stepFocusedCard(dir: 1 | -1): boolean {
  const card = document.activeElement?.closest('[data-card]')
  if (!card) return false
  const sel =
    dir === 1
      ? '[data-testid="inc"]:not(:disabled), [data-testid="occurrence"]:not(:disabled)'
      : '[data-testid="dec"]:not(:disabled), [data-testid="undo-last"]:not(:disabled)'
  const btn = card.querySelector<HTMLButtonElement>(sel)
  if (!btn) return false
  btn.click()
  return true
}

const rootEl = ref<HTMLElement | null>(null)

/**
 * Move focus to the previous/next visible card in DOM order (which equals visual order),
 * clamped at the ends. Returns false (not handled) only when there are no cards.
 */
function moveCardFocus(dir: 1 | -1): boolean {
  const cards = [...(rootEl.value?.querySelectorAll<HTMLElement>('[data-card]') ?? [])].filter(
    (c) => !c.closest('details:not([open]), [hidden], [aria-hidden="true"]'),
  )
  if (!cards.length) return false
  const cur = document.activeElement?.closest('[data-card]') as HTMLElement | null
  const i = cur ? cards.indexOf(cur) : -1
  let next: number
  if (i < 0) next = dir === 1 ? 0 : cards.length - 1
  else next = Math.min(cards.length - 1, Math.max(0, i + dir))
  const el = cards[next]
  if (el !== document.activeElement) {
    el.focus()
    el.scrollIntoView?.({ block: 'nearest' })
  }
  return true
}

useShortcut([
  catalogDef('today.cardUp', () => moveCardFocus(-1)),
  catalogDef('today.cardDown', () => moveCardFocus(1)),
  catalogDef('today.prevDay', () => store.goToPrevDay()),
  catalogDef('today.nextDay', () => store.goToNextDay()),
  catalogDef('today.goToday', () => store.goToToday()),
  catalogDef('today.toggleCard', toggleFocusedCard),
  catalogDef('today.incCard', () => stepFocusedCard(1)),
  catalogDef('today.decCard', () => stepFocusedCard(-1)),
])

onMounted(() => {
  void store.refresh(queryDate())
  projectsStore.fetchProjects()
  document.addEventListener('click', onDocClick)
  document.addEventListener('keydown', onKey)
})
onBeforeUnmount(() => {
  document.removeEventListener('click', onDocClick)
  document.removeEventListener('keydown', onKey)
})
// The store clears all plaintext synchronously when redact turns on; refetch afterwards.
watch(
  () => privacy.redact,
  (on) => {
    if (on) {
      projectDropdownOpen.value = false
      dateDropdownOpen.value = false
      quickTodoProjectId.value = null
    }
    void store.refresh()
    void projectsStore.fetchProjects()
  },
  { flush: 'sync' },
)
// Queued offline actions were confirmed: reload the server state.
watch(
  () => offline.syncedTick,
  () => store.refresh(),
)
</script>

<template>
  <div
    class="mood-bg"
    :class="`mood-${mood}`"
    :style="{ '--p': String(moodPct) }"
    aria-hidden="true"
  ></div>
  <section ref="rootEl" class="relative z-[1] mx-auto max-w-2xl space-y-4 p-4">
    <header v-if="store.view" class="flex items-center justify-between gap-3 pb-1">
      <div class="flex items-center gap-1 sm:gap-1.5 flex-nowrap shrink-0">
        <button
          type="button"
          class="btn btn-sm btn-circle btn-ghost shrink-0"
          :aria-label="t('today.prevDay')"
          data-testid="prev-day"
          @click="store.goToPrevDay()"
        >
          <Icon icon="lucide:chevron-left" class="size-5" />
        </button>
        <div class="relative flex items-center shrink-0">
          <input
            ref="datePickerInput"
            type="date"
            class="sr-only"
            :value="store.view.date"
            data-testid="today-date-picker"
            @change="store.goToDate(($event.target as HTMLInputElement).value)"
          />
          <button
            type="button"
            class="btn btn-sm btn-ghost gap-1.5 font-bold text-base sm:text-lg cursor-pointer px-2"
            :title="t('calendar.pickDate')"
            data-testid="open-date-picker"
            @click="
              datePickerInput?.showPicker ? datePickerInput.showPicker() : datePickerInput?.focus()
            "
          >
            <Icon icon="lucide:calendar" class="size-4 opacity-70 shrink-0" />
            <span class="font-mono tracking-tight">{{ formatCustomDate(store.view.date) }}</span>
            <span class="text-xs sm:text-sm font-normal text-base-content/70 whitespace-nowrap">
              {{ weekdayName(store.view.weekday) }}
            </span>
          </button>
        </div>
        <button
          type="button"
          class="btn btn-sm btn-circle btn-ghost shrink-0"
          :aria-label="t('today.nextDay')"
          data-testid="next-day"
          @click="store.goToNextDay()"
        >
          <Icon icon="lucide:chevron-right" class="size-5" />
        </button>
      </div>

      <div class="flex items-center gap-2 shrink-0">
        <span v-if="store.isToday" class="badge badge-primary badge-outline badge-sm">
          {{ t('today.todayBadge') }}
        </span>
        <button
          v-else
          type="button"
          class="btn btn-xs btn-outline btn-primary shrink-0"
          @click="store.goToToday()"
        >
          {{ t('today.jumpToday') }}
        </button>
      </div>
    </header>

    <div v-if="store.isFuture" class="alert alert-info text-xs py-2 shadow-xs" role="status">
      <Icon icon="lucide:info" class="size-4 shrink-0" />
      <span>{{ t('today.futureNotice') }}</span>
    </div>

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

    <template v-if="store.view">
      <CompletionRing
        :done="store.ring.done"
        :total="store.ring.total"
        :todo-done="store.todoRing.done"
        :todo-total="store.todoRing.total"
      />

      <CategoryFilter
        v-model="store.filter"
        :categories="store.categories"
        :total="store.categoryCounts.total"
        :counts="store.categoryCounts.counts"
      />

      <!-- Quick Add (Todoist-style) -->
      <div
        class="card card-border bg-base-100 border-base-300 p-3 shadow-xs space-y-2"
        data-testid="section-unscheduled"
      >
        <div class="relative w-full">
          <div class="flex flex-col sm:flex-row items-stretch sm:items-center gap-2">
            <!-- Text input -->
            <div class="relative flex-1">
              <input
                v-model="quickLogQuery"
                type="text"
                class="input input-sm input-bordered w-full pr-8 pl-3"
                :placeholder="
                  store.isFuture
                    ? t('today.quickAddFuturePlaceholder')
                    : t('today.quickAddPlaceholder')
                "
                autocomplete="off"
                data-testid="quick-log-input"
                data-shortcut-search
                @focus="quickLogOpen = true"
                @blur="onBlurQuickLog"
                @keydown="onQuickLogKeydown"
              />
              <button
                v-if="quickLogQuery"
                type="button"
                class="absolute right-2 top-2 text-base-content/40 hover:text-base-content/80 cursor-pointer"
                @click="quickLogQuery = ''"
              >
                <Icon icon="lucide:x" class="size-3.5" />
              </button>
            </div>

            <!-- Toolbar actions: Date Chip, Add Todo button, Quick Log Done button -->
            <div class="flex items-center gap-1.5 justify-end shrink-0">
              <!-- Date Chip with Dropdown -->
              <div ref="dateChipRef" class="relative">
                <button
                  type="button"
                  class="btn btn-sm btn-outline h-8 min-h-8 px-2.5 text-xs font-normal gap-1.5 cursor-pointer"
                  :class="{
                    'btn-primary bg-primary/10 text-primary border-primary/30': !!quickTodoDate,
                  }"
                  data-testid="quick-date-chip"
                  :title="t('today.todoDueDateOptional')"
                  @click.stop="dateDropdownOpen = !dateDropdownOpen"
                >
                  <Icon icon="lucide:calendar" class="size-3.5" />
                  <span v-if="quickTodoDate" class="font-medium">
                    {{ dateChipLabel }}
                  </span>
                  <span v-else class="text-base-content/60">
                    {{ t('today.dateOption.noDate') }}
                  </span>
                  <button
                    v-if="quickTodoDate"
                    type="button"
                    class="hover:text-error ml-0.5 p-0.5 rounded-full inline-flex items-center justify-center cursor-pointer"
                    data-testid="quick-date-clear"
                    :aria-label="t('common.clear')"
                    @click.stop="quickTodoDate = ''"
                  >
                    <Icon icon="lucide:x" class="size-3" />
                  </button>
                </button>

                <!-- Date options dropdown -->
                <div
                  v-show="dateDropdownOpen"
                  class="absolute right-0 sm:left-0 z-30 mt-1 w-56 rounded-lg border border-base-300 bg-base-100 p-2 shadow-lg space-y-1 text-sm"
                  data-testid="quick-date-dropdown"
                >
                  <button
                    type="button"
                    class="w-full text-left px-2.5 py-1.5 text-xs rounded-md hover:bg-base-200 flex items-center justify-between cursor-pointer"
                    data-testid="quick-date-today"
                    @click="setDateOption(todayDate)"
                  >
                    <span class="flex items-center gap-2">
                      <Icon icon="lucide:calendar" class="size-3.5 text-primary" />
                      <span>{{ t('today.dateOption.today') }}</span>
                    </span>
                    <span class="text-[10px] text-base-content/50 font-mono">{{
                      formatCustomDate(todayDate)
                    }}</span>
                  </button>
                  <button
                    type="button"
                    class="w-full text-left px-2.5 py-1.5 text-xs rounded-md hover:bg-base-200 flex items-center justify-between cursor-pointer"
                    data-testid="quick-date-tomorrow"
                    @click="setDateOption(tomorrowDate)"
                  >
                    <span class="flex items-center gap-2">
                      <Icon icon="lucide:sun" class="size-3.5 text-warning" />
                      <span>{{ t('today.tomorrow') }}</span>
                    </span>
                    <span class="text-[10px] text-base-content/50 font-mono">{{
                      formatCustomDate(tomorrowDate)
                    }}</span>
                  </button>
                  <button
                    type="button"
                    class="w-full text-left px-2.5 py-1.5 text-xs rounded-md hover:bg-base-200 flex items-center gap-2 cursor-pointer"
                    data-testid="quick-date-nodate"
                    @click="setDateOption('')"
                  >
                    <Icon icon="lucide:inbox" class="size-3.5 text-base-content/60" />
                    <span>{{ t('today.dateOption.noDate') }}</span>
                  </button>
                  <div class="divider my-1"></div>
                  <div class="px-1 pt-0.5" data-testid="quick-date-custom">
                    <label class="block text-[11px] font-medium text-base-content/70 mb-1">
                      {{ t('today.dateOption.custom') }}
                    </label>
                    <input
                      v-model="quickTodoDate"
                      type="date"
                      class="input input-xs input-bordered w-full font-mono text-xs"
                      data-testid="quick-todo-date"
                      @change="dateDropdownOpen = false"
                    />
                  </div>
                </div>
              </div>

              <!-- Project Chip with Dropdown -->
              <div ref="projectChipRef" class="relative">
                <button
                  type="button"
                  class="btn btn-sm btn-outline h-8 min-h-8 px-2.5 text-xs font-normal gap-1.5 cursor-pointer"
                  :class="{
                    'btn-primary bg-primary/10 text-primary border-primary/30':
                      quickTodoProjectId !== null,
                  }"
                  data-testid="quick-project-chip"
                  :title="t('projects.project')"
                  @click.stop="projectDropdownOpen = !projectDropdownOpen"
                >
                  <Icon icon="lucide:folder" class="size-3.5" />
                  <template v-if="selectedProject">
                    <span
                      v-if="selectedProject.color"
                      class="size-2 rounded-full shrink-0"
                      :style="{ backgroundColor: selectedProject.color }"
                    />
                    <span class="font-medium max-w-24 truncate">
                      {{ selectedProject.name }}
                    </span>
                    <button
                      type="button"
                      class="hover:text-error ml-0.5 p-0.5 rounded-full inline-flex items-center justify-center cursor-pointer"
                      data-testid="quick-project-clear"
                      :aria-label="t('common.clear')"
                      @click.stop="quickTodoProjectId = null"
                    >
                      <Icon icon="lucide:x" class="size-3" />
                    </button>
                  </template>
                  <span v-else class="text-base-content/60">
                    {{ t('projects.noProject') }}
                  </span>
                </button>

                <!-- Project options dropdown -->
                <div
                  v-show="projectDropdownOpen"
                  class="absolute right-0 sm:left-0 z-30 mt-1 w-56 max-h-60 overflow-y-auto rounded-lg border border-base-300 bg-base-100 p-2 shadow-lg space-y-1 text-sm"
                  data-testid="quick-project-dropdown"
                >
                  <button
                    type="button"
                    class="w-full text-left px-2.5 py-1.5 text-xs rounded-md hover:bg-base-200 flex items-center justify-between cursor-pointer"
                    data-testid="quick-project-noproject"
                    @click="setProjectOption(null)"
                  >
                    <span class="flex items-center gap-2">
                      <Icon icon="lucide:folder" class="size-3.5 text-base-content/60" />
                      <span>{{ t('projects.noProject') }}</span>
                    </span>
                    <Icon
                      v-if="quickTodoProjectId === null"
                      icon="lucide:check"
                      class="size-3 text-primary shrink-0"
                    />
                  </button>
                  <div v-if="projectsStore.activeProjects.length > 0" class="divider my-1"></div>
                  <button
                    v-for="p in projectsStore.activeProjects"
                    :key="p.id"
                    type="button"
                    class="w-full text-left px-2.5 py-1.5 text-xs rounded-md hover:bg-base-200 flex items-center justify-between cursor-pointer"
                    :data-testid="`quick-project-option-${p.id}`"
                    @click="setProjectOption(p.id)"
                  >
                    <span class="flex items-center gap-2 truncate">
                      <span
                        v-if="p.color"
                        class="size-2 rounded-full shrink-0"
                        :style="{ backgroundColor: p.color }"
                      />
                      <Icon v-else icon="lucide:folder" class="size-3.5 text-primary shrink-0" />
                      <span class="truncate">{{ p.name }}</span>
                    </span>
                    <Icon
                      v-if="quickTodoProjectId === p.id"
                      icon="lucide:check"
                      class="size-3 text-primary shrink-0"
                    />
                  </button>
                </div>
              </div>

              <!-- Add Todo button -->
              <button
                type="button"
                class="btn btn-sm btn-primary shadow-xs h-8 min-h-8 px-3 gap-1.5"
                :disabled="!quickLogQuery.trim() || quickLogBusy"
                data-testid="quick-todo-submit"
                @click="
                  submitQuickTodo({
                    name: quickLogQuery,
                    dueDate: quickTodoDate || null,
                  })
                "
              >
                <span v-if="quickLogBusy" class="loading loading-spinner loading-xs" />
                <Icon v-else icon="lucide:plus" class="size-4" />
                <span>{{ store.isFuture ? t('today.addFutureEvent') : t('today.addTodo') }}</span>
              </button>

              <!-- Quick Log as Done button -->
              <button
                type="button"
                class="btn btn-sm btn-outline shadow-xs h-8 min-h-8 px-2.5 gap-1"
                :disabled="!quickLogQuery.trim() || quickLogBusy"
                :title="t('today.quickLogAsDone')"
                data-testid="quick-log-submit"
                @click="submitQuickLog({ name: quickLogQuery })"
              >
                <Icon icon="lucide:check" class="size-4 text-success" />
                <span class="hidden sm:inline">{{
                  store.isFuture ? t('today.addFutureEvent') : t('today.logNow')
                }}</span>
              </button>
            </div>
          </div>

          <!-- Dropdown suggestions for text input -->
          <div
            v-if="
              quickLogOpen &&
              ((!store.isFuture && matchingUnscheduled.length > 0) || quickLogQuery.trim())
            "
            class="absolute left-0 right-0 z-30 mt-1 max-h-60 overflow-y-auto rounded-lg border border-base-300 bg-base-100 p-1 shadow-lg"
          >
            <!-- Create new Todo option -->
            <button
              v-if="quickLogQuery.trim()"
              type="button"
              class="w-full text-left px-3 py-2 text-sm rounded-md hover:bg-primary/10 text-primary font-medium flex items-center gap-2 cursor-pointer"
              :class="{ 'bg-primary/10': quickLogIndex === 0 }"
              data-testid="quick-create-todo"
              @mousedown.prevent="
                submitQuickTodo({
                  name: quickLogQuery,
                  dueDate: quickTodoDate || (store.isFuture ? (store.view?.date ?? null) : null),
                })
              "
            >
              <Icon icon="lucide:check-square" class="size-4 shrink-0" />
              <span>{{ t('today.createTodo', { name: quickLogQuery.trim() }) }}</span>
            </button>

            <!-- Create new option (Log Done immediately) -->
            <button
              v-if="
                quickLogQuery.trim() &&
                (store.isFuture ||
                  !matchingUnscheduled.some(
                    (it) => it.name.toLowerCase() === quickLogQuery.trim().toLowerCase(),
                  ))
              "
              type="button"
              class="w-full text-left px-3 py-2 text-sm rounded-md hover:bg-base-200 text-base-content/80 font-medium flex items-center gap-2 cursor-pointer"
              :class="{ 'bg-base-200': quickLogIndex === 1 }"
              @mousedown.prevent="submitQuickLog({ name: quickLogQuery })"
            >
              <Icon icon="lucide:sparkles" class="size-4 shrink-0" />
              <span>{{
                store.isFuture
                  ? t('today.createFutureEvent', { name: quickLogQuery.trim() })
                  : t('today.createAndLog', { name: quickLogQuery.trim() })
              }}</span>
            </button>

            <!-- Matching existing unscheduled items (today or past dates) -->
            <template v-if="!store.isFuture">
              <button
                v-for="u in matchingUnscheduled"
                :key="u.id"
                type="button"
                class="w-full text-left px-3 py-2 text-sm rounded-md hover:bg-base-200 flex items-center justify-between cursor-pointer"
                :class="{
                  'bg-base-200': quickLogOptions[quickLogIndex]?.key === `item-${u.id}`,
                }"
                @mousedown.prevent="submitQuickLog(u)"
              >
                <div class="flex items-center gap-2 min-w-0">
                  <span
                    class="font-medium text-base-content/90 truncate"
                    :class="{ sensitive: u.effective_sensitive }"
                    >{{ u.name }}</span
                  >
                  <span v-if="todayCount(u) !== null" class="badge badge-xs badge-neutral shrink-0">
                    {{ todayCount(u) }}
                  </span>
                </div>
                <span class="text-xs text-base-content/50 shrink-0">
                  {{
                    u.tracker_type === 'counter'
                      ? `+${u.step && u.step > 0 ? u.step : 1}`
                      : t('today.card.check')
                  }}
                </span>
              </button>
            </template>
          </div>

          <!-- Pinned unscheduled ongoing items for one-tap logging -->
          <div
            v-if="pinnedUnscheduledOngoing.length && !store.isFuture"
            class="flex flex-wrap items-center gap-1.5 pt-1 border-t border-base-200/80"
            data-testid="pinned-quick-logs"
          >
            <span class="text-xs text-base-content/60 font-medium flex items-center gap-1">
              <Icon icon="lucide:pin" class="size-3 text-primary" />
              {{ t('today.pinnedQuickLog') }}:
            </span>
            <button
              v-for="u in pinnedUnscheduledOngoing"
              :key="u.id"
              type="button"
              class="btn btn-xs btn-outline gap-1.5 h-7 min-h-7"
              :title="t('today.quickLogAsDone')"
              :data-testid="`pinned-log-${u.id}`"
              @click="submitQuickLog(u)"
            >
              <Icon icon="lucide:plus" class="size-3" />
              <span :class="{ sensitive: u.effective_sensitive }">{{ u.name }}</span>
              <span
                v-if="todayCount(u) !== null"
                class="badge badge-xs badge-neutral"
                :data-testid="`pinned-count-${u.id}`"
              >
                {{ todayCount(u) }}
              </span>
            </button>
          </div>
        </div>
      </div>

      <div
        v-if="store.categoryCounts.total === 0"
        class="card card-border bg-base-100 border-base-300 p-8 text-center items-center gap-3 shadow-xs"
        data-testid="empty"
      >
        <div class="size-12 rounded-full bg-base-200 flex items-center justify-center text-primary">
          <Icon icon="lucide:sparkles" class="size-6" aria-hidden="true" />
        </div>
        <p class="text-sm text-base-content/70">
          {{ t('today.empty') }}
          <RouterLink to="/items" class="link link-primary font-medium ml-1">{{
            t('today.manage')
          }}</RouterLink>
        </p>
      </div>
      <div
        v-else-if="visibleCount === 0"
        class="card card-border bg-base-100 border-base-300 p-8 text-center items-center gap-3 shadow-xs"
        data-testid="empty-filter"
      >
        <div
          class="size-12 rounded-full bg-base-200 flex items-center justify-center text-base-content/60"
        >
          <Icon icon="lucide:filter" class="size-6" aria-hidden="true" />
        </div>
        <p class="text-sm text-base-content/70">
          {{ t('today.emptyFilter') }}
        </p>
      </div>

      <!-- Overdue section: keep reminding until completed -->
      <section
        v-if="store.overdueCards.length"
        class="card card-border border-error/40 bg-error/5 p-4 shadow-xs space-y-3"
        data-testid="section-overdue"
      >
        <div class="flex items-center justify-between">
          <h2 class="text-sm font-bold text-error flex items-center gap-2">
            <Icon icon="lucide:alert-circle" class="size-4" />
            {{ t('today.section.overdue') }} ({{ store.overdueCards.length }})
          </h2>
        </div>
        <ul class="space-y-2" data-testid="overdue-list">
          <li v-for="c in overdueGrouped" :key="c.detail.id" :style="indent(c)">
            <ItemCard
              :vm="c"
              :category="cat(c)"
              :project="projectFor(c)"
              variant="todo"
              :busy="store.busy.includes(c.detail.id)"
              :pending="offline.pendingItems.includes(c.detail.id)"
              @log="log"
              @remove="store.remove(c.detail.id, $event)"
              @open-details="detailItemId = c.detail.id"
            />
          </li>
        </ul>
      </section>

      <template v-for="s in sections" :key="s.id">
        <section
          v-if="s.cards.length"
          :aria-labelledby="`h-${s.id}`"
          class="card card-border bg-base-100 border-base-300 p-4 shadow-xs space-y-3"
          :data-testid="`section-${s.id}`"
        >
          <h2
            :id="`h-${s.id}`"
            class="text-sm font-bold text-base-content/90 flex items-center gap-2"
          >
            <Icon :icon="s.icon" class="size-4 text-primary" />
            {{ t('common.countSuffix', { title: s.title, count: s.cards.length }) }}
          </h2>
          <ul class="space-y-2" :data-testid="`${s.id}-list`">
            <li
              v-for="c in s.cards"
              :key="c.detail.id"
              :style="indent(c)"
              :data-id="c.detail.id"
              :data-parent="c.detail.parent_id ?? ''"
              :data-project="c.detail.project_id ?? ''"
            >
              <ItemCard
                reorderable
                @drag-start="onDragStart($event, c)"
                :vm="c"
                :category="cat(c)"
                :project="projectFor(c)"
                :variant="isDoneCard(c) ? 'done' : s.variant"
                :busy="store.busy.includes(c.detail.id)"
                :pending="offline.pendingItems.includes(c.detail.id)"
                @log="log"
                @remove="store.remove(c.detail.id, $event)"
                @open-details="detailItemId = c.detail.id"
              />
            </li>
          </ul>
        </section>
      </template>

      <!-- Unscheduled Todos / 待办池: collapsible; completed ones stay at the end -->
      <details
        v-if="poolCards.length"
        open
        class="card card-border bg-base-100 border-base-300 shadow-xs group"
        data-testid="section-unscheduled-todos"
      >
        <summary
          class="p-4 cursor-pointer flex items-center justify-between gap-2 select-none list-none"
        >
          <h2 class="text-sm font-bold text-base-content/90 flex items-center gap-2">
            <Icon icon="lucide:inbox" class="size-4 text-primary" />
            {{ t('today.section.unscheduledTodos') }} ({{ poolCards.length }})
          </h2>
          <span class="flex items-center gap-2">
            <span class="hidden sm:inline text-xs text-base-content/60">{{
              t('today.unscheduledTodosHint')
            }}</span>
            <Icon
              icon="lucide:chevron-down"
              class="size-4 opacity-60 transition-transform duration-200 group-open:rotate-180"
              aria-hidden="true"
            />
          </span>
        </summary>
        <div class="px-4 pb-4">
          <ul class="space-y-2" data-testid="unscheduled-todos-list">
            <li
              v-for="c in poolCards"
              :key="c.detail.id"
              :style="indent(c)"
              :data-id="c.detail.id"
              :data-parent="c.detail.parent_id ?? ''"
              :data-project="c.detail.project_id ?? ''"
            >
              <ItemCard
                reorderable
                @drag-start="onDragStart($event, c)"
                :vm="c"
                :category="cat(c)"
                :project="projectFor(c)"
                :variant="isDoneCard(c) ? 'done' : 'todo'"
                :busy="store.busy.includes(c.detail.id)"
                :pending="offline.pendingItems.includes(c.detail.id)"
                @log="log"
                @remove="store.remove(c.detail.id, $event)"
                @open-details="detailItemId = c.detail.id"
              />
            </li>
          </ul>
        </div>
      </details>

      <!-- Goals ("at least N per period"): always visible with progress -->
      <div
        v-if="store.goalCards.length"
        class="card card-border bg-base-100 border-base-300 p-4 shadow-xs space-y-3"
        data-testid="section-goals"
      >
        <h2 class="text-sm font-bold text-base-content/90 flex items-center gap-2">
          <Icon icon="lucide:target" class="size-4 text-primary" aria-hidden="true" />
          {{
            t('common.countSuffix', {
              title: t('today.section.goals'),
              count: store.goalCards.length,
            })
          }}
        </h2>
        <ul class="space-y-2" data-testid="goal-list">
          <li v-for="c in store.goalCards" :key="c.detail.id">
            <ItemCard
              :vm="c"
              :category="cat(c)"
              :project="projectFor(c)"
              variant="quota"
              :date="store.view?.date"
              :busy="store.busy.includes(c.detail.id)"
              :pending="offline.pendingItems.includes(c.detail.id)"
              @log="log"
              @remove="store.remove(c.detail.id, $event)"
              @open-details="detailItemId = c.detail.id"
            />
          </li>
        </ul>
      </div>

      <!-- Limits ("at most N per period", e.g. cola): collapsed so they do not nag -->
      <details
        v-if="store.quotaCards.length"
        :open="store.quotaUrgentCount > 0 || undefined"
        class="card card-border bg-base-100/60 border-base-300/80 shadow-xs group"
        data-testid="section-quota"
      >
        <summary
          class="p-3 cursor-pointer font-semibold text-sm flex items-center justify-between select-none list-none"
        >
          <span class="flex items-center gap-2">
            <Icon icon="lucide:gauge" class="size-4 opacity-70" aria-hidden="true" />
            {{
              t('common.countSuffix', {
                title: t('today.section.quota'),
                count: store.quotaCards.length,
              })
            }}
            <span
              v-if="store.quotaUrgentCount"
              class="badge badge-warning badge-sm"
              data-testid="quota-urgent"
              >{{ t('today.quota.urgent', { count: store.quotaUrgentCount }) }}</span
            >
          </span>
          <Icon
            icon="lucide:chevron-down"
            class="size-4 opacity-60 transition-transform duration-200 group-open:rotate-180"
            aria-hidden="true"
          />
        </summary>
        <div class="p-3 pt-0">
          <ul class="space-y-2" data-testid="quota-list">
            <li v-for="c in store.quotaCards" :key="c.detail.id">
              <ItemCard
                :vm="c"
                :category="cat(c)"
                :project="projectFor(c)"
                variant="quota"
                :date="store.view?.date"
                :busy="store.busy.includes(c.detail.id)"
                :pending="offline.pendingItems.includes(c.detail.id)"
                @log="log"
                @remove="store.remove(c.detail.id, $event)"
                @open-details="detailItemId = c.detail.id"
              />
            </li>
          </ul>
        </div>
      </details>

      <!-- Upcoming section: next 3 / 7 days due -->
      <div
        v-if="store.upcoming.length"
        class="card card-border bg-base-100 border-base-300 p-4 shadow-xs space-y-3"
        data-testid="section-upcoming"
      >
        <div class="flex items-center justify-between">
          <h3 class="text-sm font-semibold text-base-content/80 flex items-center gap-2">
            <Icon icon="lucide:calendar-clock" class="size-4 text-primary" />
            {{ t('today.upcomingTitle') }}
          </h3>
          <div class="join">
            <button
              type="button"
              class="btn btn-xs join-item"
              :class="{ 'btn-primary': upcomingDays === 3, 'btn-ghost': upcomingDays !== 3 }"
              @click="upcomingDays = 3"
            >
              {{ t('today.next3Days') }}
            </button>
            <button
              type="button"
              class="btn btn-xs join-item"
              :class="{ 'btn-primary': upcomingDays === 7, 'btn-ghost': upcomingDays !== 7 }"
              @click="upcomingDays = 7"
            >
              {{ t('today.next7Days') }}
            </button>
            <button
              type="button"
              class="btn btn-xs join-item"
              :class="{ 'btn-primary': upcomingDays === 14, 'btn-ghost': upcomingDays !== 14 }"
              @click="upcomingDays = 14"
            >
              {{ t('today.next14Days') }}
            </button>
          </div>
        </div>
        <ul class="divide-y divide-base-200">
          <li
            v-for="u in filteredUpcoming"
            :key="`${u.id}-${u.due_date}`"
            class="flex items-center justify-between py-2 text-sm"
          >
            <span class="font-medium text-base-content/90">{{ u.name }}</span>
            <div class="flex items-center gap-2">
              <span class="text-xs text-base-content/60 font-mono">{{
                formatCustomDate(u.due_date)
              }}</span>
              <span class="badge badge-sm badge-ghost font-mono">
                {{
                  u.days_until === 1 ? t('today.tomorrow') : t('today.inNDays', { n: u.days_until })
                }}
              </span>
            </div>
          </li>
        </ul>
      </div>

      <OccurrenceDetailModal
        :open="!!detailCard"
        :detail="detailCard?.detail ?? null"
        :logs="detailCard?.logs ?? []"
        :date="store.view?.date ?? ''"
        :busy="detailCard ? store.busy.includes(detailCard.detail.id) : false"
        :redact="privacy.redact"
        @close="detailItemId = null"
        @log="log"
        @remove="detailCard && store.remove(detailCard.detail.id, $event)"
        @patch-note="(id, note) => store.updateNote(id, note)"
        @set-due-date="(itemId, date) => store.setDueDate(itemId, date)"
      />
    </template>
  </section>
</template>

<style scoped>
.drop-before {
  box-shadow: 0 -2px 0 0 var(--color-primary);
}
.drop-after {
  box-shadow: 0 2px 0 0 var(--color-primary);
}
</style>
