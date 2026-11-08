<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { Icon } from '@iconify/vue'
import { usePrivacyStore } from '../stores/privacy'
import { useItemDetailStore, useItemsStore } from '../stores/items'
import { describeRule, shiftMonth } from '../items/logic'
import Heatmap from '../components/Heatmap.vue'
import { getCalendarSettings } from '../api'

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
  .then((s) => { if (s.week_start === 0 || s.week_start === 1) weekStart.value = s.week_start })
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
const pct = (n: number | null) => (n === null ? 'n/a' : `${Math.round(n * 100)}%`)
</script>

<template>
  <section class="space-y-4" data-testid="detail">
    <RouterLink to="/items" class="btn btn-ghost btn-sm lg:hidden" data-testid="back">
      <Icon icon="lucide:chevron-left" aria-hidden="true" /> Items
    </RouterLink>

    <div v-if="store.error" class="alert alert-error" role="alert" data-testid="detail-error">
      <span>{{ store.error }}</span>
      <button v-if="!store.notFound" type="button" class="btn btn-sm" @click="load">Retry</button>
    </div>
    <p v-if="store.loading && !store.item" role="status" class="opacity-70">Loading...</p>

    <template v-if="store.item && store.stats">
      <header class="flex flex-wrap items-center justify-between gap-2">
        <h2 class="text-xl font-bold" data-testid="item-name">{{ store.item.name }}</h2>
        <div class="flex flex-wrap gap-2">
          <button
            type="button" class="btn btn-sm" :class="{ 'btn-primary': store.item.pinned }" data-testid="pin"
            :aria-pressed="!!store.item.pinned" @click="togglePin"
          >
            <Icon icon="lucide:pin" aria-hidden="true" /> {{ store.item.pinned ? 'Unpin' : 'Pin' }}
          </button>
          <RouterLink :to="`/items/${store.item.id}/edit`" class="btn btn-sm" data-testid="edit">
            <Icon icon="lucide:pencil" aria-hidden="true" /> Edit expectation
          </RouterLink>
        </div>
      </header>
      <p v-if="store.item.description" class="opacity-80">{{ store.item.description }}</p>

      <div class="stats stats-vertical w-full border sm:stats-horizontal" data-testid="stats">
        <div class="stat">
          <div class="stat-title">Last</div>
          <div class="stat-value text-lg" data-testid="stat-last">
            {{ store.stats.days_since_last === null ? 'Never' : `${store.stats.days_since_last} days ago` }}
          </div>
          <div class="stat-desc">{{ store.stats.last_date ?? '' }}</div>
        </div>
        <div v-if="store.stats.streak.applicable" class="stat">
          <div class="stat-title">Streak</div>
          <div class="stat-value text-lg" data-testid="stat-streak">{{ store.stats.streak.current }} {{ store.stats.streak.unit ?? '' }}</div>
          <div class="stat-desc">Longest {{ store.stats.streak.longest }}</div>
        </div>
        <div class="stat">
          <div class="stat-title">Completion (90d)</div>
          <div class="stat-value text-lg" data-testid="stat-completion">{{ pct(store.stats.completion_rate_90) }}</div>
        </div>
        <div class="stat">
          <div class="stat-title">Check-ins</div>
          <div class="stat-value text-lg">{{ store.stats.counts.total }}</div>
          <div class="stat-desc">{{ store.stats.counts.last_30_days }} in 30 days, {{ store.stats.counts.this_year }} this year</div>
        </div>
      </div>
      <p v-if="store.stats.summary" class="text-sm opacity-80">{{ store.stats.summary }}</p>

      <section>
        <h3 class="mb-1 font-semibold">Schedule</h3>
        <p data-testid="rule">{{ rule }}</p>
        <p class="text-sm opacity-70" data-testid="rule-note">
          Editing the expectation creates a new rule version that takes effect today. Past results stay as they were.
        </p>
      </section>

      <section>
        <h3 class="mb-1 font-semibold">Last 365 days</h3>
        <Heatmap
          :days="store.stats.heatmap" :week-start="weekStart" :negative="store.item?.polarity === 'negative'"
          :redact="privacy.redact" label="Activity heatmap for the last 365 days"
        />
      </section>

      <section>
        <div class="mb-1 flex items-center justify-between">
          <h3 class="font-semibold">Calendar {{ store.month }}</h3>
          <div class="join">
            <button type="button" class="btn btn-sm join-item" aria-label="Previous month" data-testid="cal-prev" @click="store.setMonth(shiftMonth(store.month, -1))">
              <Icon icon="lucide:chevron-left" aria-hidden="true" />
            </button>
            <button type="button" class="btn btn-sm join-item" aria-label="Next month" data-testid="cal-next" @click="store.setMonth(shiftMonth(store.month, 1))">
              <Icon icon="lucide:chevron-right" aria-hidden="true" />
            </button>
          </div>
        </div>
        <div class="grid grid-cols-7 gap-1 text-center text-xs" data-testid="calendar">
          <template v-for="(d, i) in calendarCells" :key="i">
            <span v-if="d" class="rounded p-1" :class="dayClass(d)" :data-date="d.date">{{ Number(d.date.slice(8)) }}</span>
            <span v-else></span>
          </template>
        </div>
        <p class="mt-1 text-xs opacity-70">Filled: done. Dashed: expected. Red: overdue.</p>
      </section>

      <section>
        <h3 class="mb-1 font-semibold">History</h3>
        <p v-if="!store.history.length" class="opacity-70" data-testid="history-empty">No check-ins yet.</p>
        <ul class="divide-y" data-testid="history">
          <li v-for="l in store.history" :key="l.id" class="flex items-center justify-between gap-2 py-1" :data-testid="`log-${l.id}`">
            <span class="min-w-0 truncate">
              {{ l.date }} <span v-if="optionName(l.option_id)">{{ optionName(l.option_id) }}</span>
              <span v-if="store.item.tracker_type !== 'boolean' && store.item.tracker_type !== 'maintenance'"> {{ l.value }}</span>
              <span v-if="l.note" class="opacity-70"> {{ l.note }}</span>
              <span v-if="l.is_supplement" class="badge badge-ghost badge-sm ml-1">backfilled</span>
            </span>
            <button
              type="button" class="btn btn-ghost btn-xs" :class="{ 'btn-error': pendingLog === l.id }"
              :data-testid="`del-${l.id}`" @click="removeLog(l.id)"
            >
              <Icon icon="lucide:trash-2" aria-hidden="true" /> {{ pendingLog === l.id ? 'Confirm' : 'Delete' }}
            </button>
          </li>
        </ul>
        <button v-if="store.nextCursor" type="button" class="btn btn-sm mt-2" data-testid="more" @click="store.loadMore()">Load more</button>
      </section>

      <section class="flex flex-wrap gap-2 border-t pt-3">
        <button type="button" class="btn btn-sm" data-testid="archive" @click="toggleArchive">
          <Icon icon="lucide:archive" aria-hidden="true" /> {{ store.item.archived ? 'Unarchive' : 'Archive' }}
        </button>
        <button v-if="!confirmDelete" type="button" class="btn btn-sm btn-outline btn-error" data-testid="delete" @click="confirmDelete = true">
          <Icon icon="lucide:trash-2" aria-hidden="true" /> Delete item
        </button>
        <template v-else>
          <span class="self-center text-sm">Delete this item and all its history permanently?</span>
          <button type="button" class="btn btn-sm btn-error" data-testid="delete-confirm" @click="removeItem">Delete permanently</button>
          <button type="button" class="btn btn-sm" @click="confirmDelete = false">Cancel</button>
        </template>
      </section>
    </template>
  </section>
</template>
