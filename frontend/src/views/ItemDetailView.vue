<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { Icon } from '@iconify/vue'
import { usePrivacyStore } from '../stores/privacy'
import { useItemDetailStore, useItemsStore } from '../stores/items'
import { describeRule, shiftMonth } from '../items/logic'
import Heatmap from '../components/Heatmap.vue'
import { getCalendarSettings } from '../api'
import { useI18n } from '../i18n'
import { formatMonthYear } from '../i18n/format'

const { t, tn } = useI18n()

const route = useRoute()
const router = useRouter()
const privacy = usePrivacyStore()
const store = useItemDetailStore()
const panel = useItemsStore()

const itemId = computed(() => Number(route.params.id))
const confirmDelete = ref(false)
const pendingLog = ref(0)

const weekStart = ref(1)
getCalendarSettings()
  .then((s) => {
    if (s.week_start === 0 || s.week_start === 1) weekStart.value = s.week_start
  })
  .catch(() => {})
const rule = computed(() => (store.item ? describeRule(store.item.current_rule) : ''))
const optionName = (id: number | null) =>
  id === null ? '' : (store.item?.options.find((o) => o.id === id)?.name ?? '')

// Leading blanks so the month grid starts on its weekday (Sunday first).
const calendarCells = computed(() => {
  const days = store.calendar
  if (!days.length) return []
  return [...Array<null>(days[0].weekday).fill(null), ...days]
})

function dayClass(d: (typeof store.calendar)[number]): string {
  const it = d.items[0]
  if (!it) return d.frozen ? 'bg-info/20' : ''
  if (it.done) return 'bg-success text-success-content'
  if (it.overdue) return 'bg-error text-error-content'
  if (it.forecast) return 'border border-dashed border-primary'
  if (it.due) return 'bg-warning/60'
  return ''
}

async function load() {
  if (!itemId.value) return
  confirmDelete.value = false
  await store.load(itemId.value)
}
watch(itemId, load, { immediate: true })
// Plaintext is dropped synchronously by the store; fetch the masked version afterwards.
watch(() => privacy.redact, load)

async function togglePin() {
  await store.patch({ pinned: !store.item?.pinned })
  void panel.refresh()
}
async function toggleArchive() {
  await store.patch({ archived: !store.item?.archived })
  void panel.refresh()
}
async function removeItem() {
  if (await store.remove()) {
    void panel.refresh()
    await router.push('/items')
  } else {
    confirmDelete.value = false
  }
}
async function removeLog(id: number) {
  if (pendingLog.value !== id) {
    pendingLog.value = id
    return
  }
  pendingLog.value = 0
  await store.removeCheckin(id)
}
const pct = (n: number | null) => (n === null ? t('common.na') : `${Math.round(n * 100)}%`)

// Built here from the stats numbers (not the server's English summary) so it follows the UI language.
const summary = computed(() => {
  const s = store.stats
  if (!s) return ''
  if (s.days_since_last === null || !s.last_date) return t('items.summary.none')
  const date = s.last_date
  if (s.days_since_last === 0) return t('items.summary.today', { date })
  if (s.days_since_last === 1) return t('items.summary.yesterday', { date })
  return tn('items.summary.ago', s.days_since_last, { date })
})
const streakText = computed(() => {
  const st = store.stats?.streak
  if (!st) return ''
  if (st.unit === 'day') return tn('items.span.day', st.current)
  if (st.unit === 'period') return tn('items.span.period', st.current)
  return String(st.current)
})
</script>

<template>
  <section class="space-y-4" data-testid="detail">
    <RouterLink to="/items" class="btn btn-ghost btn-sm lg:hidden" data-testid="back">
      <Icon icon="lucide:chevron-left" aria-hidden="true" /> {{ t('nav.items') }}
    </RouterLink>

    <div v-if="store.error" class="alert alert-error" role="alert" data-testid="detail-error">
      <span>{{ store.error }}</span>
      <button v-if="!store.notFound" type="button" class="btn btn-sm" @click="load">
        {{ t('common.retry') }}
      </button>
    </div>
    <p v-if="store.loading && !store.item" role="status" class="opacity-70">
      {{ t('common.loading') }}
    </p>

    <template v-if="store.item && store.stats">
      <!-- Header Card -->
      <div class="card card-border bg-base-100 border-base-300 p-5 shadow-sm space-y-3">
        <header class="flex flex-wrap items-center justify-between gap-3">
          <div class="flex items-center gap-2">
            <h2 class="text-xl font-bold tracking-tight" data-testid="item-name">
              {{ store.item.name }}
            </h2>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <button
              type="button"
              class="btn btn-sm shadow-xs"
              :class="{ 'btn-primary': store.item.pinned }"
              data-testid="pin"
              :aria-pressed="!!store.item.pinned"
              @click="togglePin"
            >
              <Icon icon="lucide:pin" aria-hidden="true" />
              {{ store.item.pinned ? t('items.unpin') : t('items.pin') }}
            </button>
            <RouterLink
              v-if="store.item.kind !== 'event'"
              :to="`/items/${store.item.id}/edit`"
              class="btn btn-sm shadow-xs"
              data-testid="edit"
            >
              <Icon icon="lucide:pencil" aria-hidden="true" />
              {{ t('items.detail.editExpectation') }}
            </RouterLink>
          </div>
        </header>
        <p v-if="store.item.description" class="text-sm opacity-80">
          {{ store.item.description }}
        </p>

        <!-- Stats Block -->
        <div
          class="stats stats-vertical w-full border border-base-300 bg-base-200/40 rounded-xl shadow-xs sm:stats-horizontal"
          data-testid="stats"
        >
          <div class="stat">
            <div class="stat-title text-xs font-medium">{{ t('items.detail.last') }}</div>
            <div class="stat-value text-lg" data-testid="stat-last">
              {{
                store.stats.days_since_last === null
                  ? t('items.detail.never')
                  : tn('items.detail.daysAgo', store.stats.days_since_last)
              }}
            </div>
            <div class="stat-desc">{{ store.stats.last_date ?? '' }}</div>
          </div>
          <div v-if="store.stats.streak.applicable" class="stat">
            <div class="stat-title text-xs font-medium">{{ t('items.detail.streak') }}</div>
            <div class="stat-value text-lg" data-testid="stat-streak">{{ streakText }}</div>
            <div class="stat-desc">
              {{ t('items.detail.longest', { count: store.stats.streak.longest }) }}
            </div>
          </div>
          <div v-if="store.stats.completion_rate_90 !== null" class="stat">
            <div class="stat-title text-xs font-medium">{{ t('items.detail.completion') }}</div>
            <div class="stat-value text-lg text-primary" data-testid="stat-completion">
              {{ pct(store.stats.completion_rate_90) }}
            </div>
          </div>
          <div class="stat">
            <div class="stat-title text-xs font-medium">{{ t('items.detail.checkins') }}</div>
            <div class="stat-value text-lg">{{ store.stats.counts.total }}</div>
            <div class="stat-desc">
              {{
                t('items.detail.checkinsDesc', {
                  last30: store.stats.counts.last_30_days,
                  year: store.stats.counts.this_year,
                })
              }}
            </div>
          </div>
        </div>
        <p class="text-xs text-base-content/70 flex items-center gap-1.5" data-testid="summary">
          <Icon icon="lucide:info" class="size-3.5 shrink-0 opacity-60" aria-hidden="true" />
          {{ summary }}
        </p>
      </div>

      <!-- Schedule & Heatmap Card -->
      <div class="card card-border bg-base-100 border-base-300 p-5 shadow-sm space-y-4">
        <section>
          <h3 class="mb-1 text-xs font-semibold uppercase tracking-wider text-base-content/70">
            {{ t('items.detail.schedule') }}
          </h3>
          <p class="font-medium" data-testid="rule">{{ rule }}</p>
          <p class="text-xs text-base-content/60 mt-0.5" data-testid="rule-note">
            {{ t('items.detail.ruleNote') }}
          </p>
        </section>

        <section class="border-t border-base-300/60 pt-4">
          <h3 class="mb-2 text-xs font-semibold uppercase tracking-wider text-base-content/70">
            {{ t('items.detail.last365') }}
          </h3>
          <Heatmap
            :days="store.stats.heatmap"
            :week-start="weekStart"
            :negative="store.item?.polarity === 'negative'"
            :redact="privacy.redact"
            :label="t('items.detail.heatmapLabel')"
          />
        </section>
      </div>

      <!-- Month Calendar & History Card -->
      <div class="card card-border bg-base-100 border-base-300 p-5 shadow-sm space-y-5">
        <section>
          <div class="mb-3 flex items-center justify-between">
            <h3 class="text-xs font-semibold uppercase tracking-wider text-base-content/70">
              {{ t('items.detail.calendar', { month: formatMonthYear(`${store.month}-01`) }) }}
            </h3>
            <div class="join shadow-xs">
              <button
                type="button"
                class="btn btn-xs join-item"
                :aria-label="t('items.detail.prevMonth')"
                data-testid="cal-prev"
                @click="store.setMonth(shiftMonth(store.month, -1))"
              >
                <Icon icon="lucide:chevron-left" aria-hidden="true" />
              </button>
              <button
                type="button"
                class="btn btn-xs join-item"
                :aria-label="t('items.detail.nextMonth')"
                data-testid="cal-next"
                @click="store.setMonth(shiftMonth(store.month, 1))"
              >
                <Icon icon="lucide:chevron-right" aria-hidden="true" />
              </button>
            </div>
          </div>
          <div class="grid grid-cols-7 gap-1.5 text-center text-xs" data-testid="calendar">
            <template v-for="(d, i) in calendarCells" :key="i">
              <span
                v-if="d"
                class="rounded-md p-1.5 transition-colors font-medium"
                :class="dayClass(d)"
                :data-date="d.date"
                >{{ Number(d.date.slice(8)) }}</span
              >
              <span v-else></span>
            </template>
          </div>
          <p class="mt-2 text-xs opacity-60">{{ t('items.detail.legend') }}</p>
        </section>

        <section class="border-t border-base-300/60 pt-4">
          <h3 class="mb-2 text-xs font-semibold uppercase tracking-wider text-base-content/70">
            {{ t('items.detail.history') }}
          </h3>
          <p
            v-if="!store.history.length"
            class="text-xs text-base-content/60 py-2"
            data-testid="history-empty"
          >
            {{ t('items.detail.noCheckins') }}
          </p>
          <ul class="divide-y divide-base-300/60" data-testid="history">
            <li
              v-for="l in store.history"
              :key="l.id"
              class="flex items-center justify-between gap-2 py-2"
              :data-testid="`log-${l.id}`"
            >
              <span class="min-w-0 truncate text-sm">
                <span class="font-medium">{{ l.date }}</span>
                <span v-if="optionName(l.option_id)" class="ml-1 text-base-content/80">{{
                  optionName(l.option_id)
                }}</span>
                <span
                  v-if="
                    store.item.tracker_type !== 'boolean' &&
                    store.item.tracker_type !== 'maintenance'
                  "
                  class="ml-1 font-semibold"
                >
                  {{ l.value }}</span
                >
                <span v-if="l.note" class="opacity-70 ml-1"> &middot; {{ l.note }}</span>
                <span v-if="l.is_supplement" class="badge badge-ghost badge-xs ml-1.5">{{
                  t('items.detail.backfilled')
                }}</span>
              </span>
              <button
                type="button"
                class="btn btn-ghost btn-xs"
                :class="{ 'btn-error': pendingLog === l.id }"
                :data-testid="`del-${l.id}`"
                @click="removeLog(l.id)"
              >
                <Icon icon="lucide:trash-2" aria-hidden="true" />
                {{ pendingLog === l.id ? t('items.detail.confirm') : t('common.delete') }}
              </button>
            </li>
          </ul>
          <button
            v-if="store.nextCursor"
            type="button"
            class="btn btn-sm btn-ghost border-base-300 mt-2"
            data-testid="more"
            @click="store.loadMore()"
          >
            {{ t('common.loadMore') }}
          </button>
        </section>
      </div>

      <!-- Management / Danger Actions -->
      <section class="flex flex-wrap items-center gap-2 pt-1">
        <button
          type="button"
          class="btn btn-sm btn-outline"
          data-testid="archive"
          @click="toggleArchive"
        >
          <Icon icon="lucide:archive" aria-hidden="true" />
          {{ store.item.archived ? t('items.detail.unarchive') : t('items.detail.archive') }}
        </button>
        <button
          v-if="!confirmDelete"
          type="button"
          class="btn btn-sm btn-outline btn-error"
          data-testid="delete"
          @click="confirmDelete = true"
        >
          <Icon icon="lucide:trash-2" aria-hidden="true" /> {{ t('items.detail.deleteItem') }}
        </button>
        <template v-else>
          <span class="self-center text-sm text-error font-medium">{{
            t('items.detail.deleteAsk')
          }}</span>
          <button
            type="button"
            class="btn btn-sm btn-error shadow-xs"
            data-testid="delete-confirm"
            @click="removeItem"
          >
            {{ t('items.detail.deleteConfirm') }}
          </button>
          <button type="button" class="btn btn-sm btn-ghost" @click="confirmDelete = false">
            {{ t('common.cancel') }}
          </button>
        </template>
      </section>
    </template>
  </section>
</template>
