<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { Icon } from '@iconify/vue'
import { addDays, periodTitle, weekdayHeaders, type CalendarMode } from '../calendar/logic'
import { useOfflineStore } from '../stores/offline'
import { usePrivacyStore } from '../stores/privacy'
import { useCalendarStore } from '../stores/calendar'
import DayCell from '../components/calendar/DayCell.vue'
import DayPanel from '../components/calendar/DayPanel.vue'

const privacy = usePrivacyStore()
const store = useCalendarStore()

const gridEl = ref<HTMLElement | null>(null)
const focusDate = ref(store.anchor)

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

async function navigate(n: number) {
  await store.page(n)
  if (!inRange(focusDate.value)) focusDate.value = store.range.dates[0] ?? store.anchor
}

async function moveFocus(target: string) {
  focusDate.value = target
  if (!inRange(target)) {
    store.anchor = target
    await store.refresh()
  }
  await focusCell(target)
}

function onGridKey(e: KeyboardEvent) {
  if (e.altKey || e.ctrlKey || e.metaKey) return
  const step: Record<string, number> = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 }
  if (e.key in step) {
    e.preventDefault()
    void moveFocus(addDays(focusDate.value, step[e.key]!))
  } else if (e.key === 'PageUp' || e.key === 'PageDown') {
    e.preventDefault()
    const n = e.key === 'PageUp' ? -1 : 1
    void store.page(n).then(() => {
      focusDate.value = store.range.dates.includes(focusDate.value) ? focusDate.value : (store.range.dates[0] ?? store.anchor)
    })
  } else if (e.key === 'Escape' && store.selected) {
    const d = store.selected
    void store.select(null)
    void focusCell(d)
  }
}

function select(date: string) {
  focusDate.value = date
  void store.select(store.selected === date ? null : date)
}

function setMode(m: CalendarMode) {
  void store.setMode(m)
}

onMounted(() => {
  void store.refresh().then(() => {
    focusDate.value = store.view?.today && inRange(store.view.today) ? store.view.today : store.range.dates[0] ?? store.anchor
  })
})
// The store drops all plaintext synchronously when redact turns on; refetch afterwards.
watch(() => useOfflineStore().syncedTick, () => {
  void store.refresh()
  void store.loadPanel()
})
watch(() => privacy.redact, () => {
  void store.refresh()
  void store.loadPanel()
})
</script>

<template>
  <section class="mx-auto max-w-6xl space-y-3 p-4">
    <header class="flex flex-wrap items-center justify-between gap-2">
      <h1 class="text-2xl font-bold">Calendar</h1>
      <div class="flex flex-wrap items-center gap-2">
        <div class="join" role="group" aria-label="Calendar view">
          <button
            v-for="m in (['month', 'week'] as const)" :key="m" type="button" class="btn btn-sm join-item"
            :class="store.mode === m ? 'btn-primary' : ''" :aria-pressed="store.mode === m" :data-testid="`mode-${m}`"
            @click="setMode(m)"
          >{{ m === 'month' ? 'Month' : 'Week' }}</button>
        </div>
        <button type="button" class="btn btn-sm" data-testid="go-today" @click="store.goToday()">Today</button>
        <div class="join">
          <button type="button" class="btn btn-sm btn-square join-item" :aria-label="`Previous ${store.mode}`" data-testid="prev" @click="navigate(-1)">
            <Icon icon="lucide:chevron-left" aria-hidden="true" />
          </button>
          <button type="button" class="btn btn-sm btn-square join-item" :aria-label="`Next ${store.mode}`" data-testid="next" @click="navigate(1)">
            <Icon icon="lucide:chevron-right" aria-hidden="true" />
          </button>
        </div>
      </div>
    </header>

    <h2 class="text-lg font-semibold" aria-live="polite" data-testid="period-title">{{ title }}</h2>

    <p v-if="store.loading && !store.view" role="status" class="py-8 text-center opacity-70" data-testid="loading">
      <span class="loading loading-spinner loading-sm" aria-hidden="true"></span> Loading...
    </p>

    <div v-if="store.error" class="alert alert-error" role="alert" data-testid="error">
      <span>{{ store.error }}</span>
      <button type="button" class="btn btn-sm" data-testid="retry" @click="store.refresh()">Retry</button>
    </div>

    <p v-if="store.view && !store.view.holiday_published" class="text-sm opacity-70" data-testid="holiday-unpublished">
      The holiday schedule is not published for this period; Off / Work badges may be incomplete.
    </p>

    <div v-if="failedSources.length || loadingSources.length" class="space-y-1 text-sm" data-testid="external-status">
      <p v-for="l in failedSources" :key="l.id" class="text-warning" role="status" :data-testid="`ext-error-${l.id}`">
        <Icon icon="lucide:triangle-alert" aria-hidden="true" class="inline" />
        External calendar "{{ l.name }}" could not be loaded: {{ l.error }}
      </p>
      <p v-if="loadingSources.length" class="opacity-70" role="status" data-testid="ext-loading">
        Loading external calendars...
      </p>
    </div>

    <div class="grid gap-3 lg:grid-cols-[1fr_22rem]">
      <div v-if="store.view" class="min-w-0" :class="store.loading ? 'opacity-70' : ''">
        <div
          ref="gridEl" role="grid" :aria-label="`${title} calendar`" :aria-busy="store.loading" data-testid="grid"
          @keydown="onGridKey"
        >
          <div role="row" class="grid grid-cols-7">
            <div v-for="h in headers" :key="h.long" role="columnheader" class="py-1 text-center text-xs font-semibold opacity-70">
              <abbr :title="h.long" class="no-underline">{{ h.short }}</abbr>
            </div>
          </div>
          <div v-for="(r, ri) in rows" :key="ri" role="row" class="grid grid-cols-7">
            <DayCell
              v-for="d in r" :key="d" :date="d" :day="store.dayByDate.get(d) ?? null" :today="todayStr"
              :in-month="store.mode === 'week' || d.startsWith(monthPrefix)" :selected="store.selected === d"
              :focusable="focusDate === d" :expanded="store.mode === 'week'" :external="store.externalByDate.get(d) ?? []"
              @select="select"
            />
          </div>
        </div>
        <ul class="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs opacity-80" aria-label="Legend">
          <li><span class="inline-block size-2 rounded-full bg-success"></span> done</li>
          <li><span class="inline-block size-2 rounded-full bg-warning"></span> partial</li>
          <li><span class="inline-block size-2 rounded-full bg-base-content/40"></span> missed</li>
          <li><span class="inline-block size-2 rounded-full bg-primary"></span> open</li>
          <li><span class="inline-block size-2 rounded-full border border-base-content/50"></span> forecast</li>
          <li><span class="inline-block size-2 rounded-full bg-error"></span> overdue</li>
          <li>Keys: arrows move, PageUp/PageDown change {{ store.mode }}, Enter opens a day</li>
        </ul>
      </div>
      <DayPanel v-if="store.selected" @close="select(store.selected!)" />
    </div>
  </section>
</template>
