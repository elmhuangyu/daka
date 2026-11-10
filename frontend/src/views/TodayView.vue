<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { Icon } from '@iconify/vue'
import { useOfflineStore } from '../stores/offline'
import { usePrivacyStore } from '../stores/privacy'
import { useTodayStore } from '../stores/today'
import { findCategory, type CardVM } from '../today/logic'
import CompletionRing from '../components/today/CompletionRing.vue'
import CategoryFilter from '../components/today/CategoryFilter.vue'
import ItemCard from '../components/today/ItemCard.vue'
import OccurrenceDetailModal from '../components/today/OccurrenceDetailModal.vue'
import { createAdHocEvent, createScheduledEvent, describeError, type CheckinInput } from '../api'
import { useI18n } from '../i18n'
import { formatCustomDate, weekdayName } from '../i18n/format'

const { t } = useI18n()

const privacy = usePrivacyStore()
const store = useTodayStore()
const offline = useOfflineStore()

const datePickerInput = ref<HTMLInputElement | null>(null)
const upcomingDays = ref<3 | 7 | 14>(7)

const filteredUpcoming = computed(() =>
  store.upcoming.filter((u) => u.days_until <= upcomingDays.value),
)

const quickLogQuery = ref('')
const quickLogOpen = ref(false)
const quickLogBusy = ref(false)

const matchingUnscheduled = computed(() => {
  const q = quickLogQuery.value.trim().toLowerCase()
  if (!q) return store.unscheduledItems
  return store.unscheduledItems.filter((it) => it.name.toLowerCase().includes(q))
})

function onBlurQuickLog() {
  setTimeout(() => {
    quickLogOpen.value = false
  }, 200)
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
      if (!itemId) {
        const exact = matchingUnscheduled.value.find(
          (it) => it.name.toLowerCase() === name.toLowerCase(),
        )
        if (exact) {
          itemId = exact.id
        }
      }
      if (itemId) {
        await store.submit({ item_id: itemId, mode: 'set', value: 1 })
      } else {
        const created = await createAdHocEvent(name)
        await store.submit({ item_id: created.id, mode: 'set', value: 1 })
      }
    }
    quickLogQuery.value = ''
    quickLogOpen.value = false
    await store.refresh(targetDate)
  } catch (e) {
    store.error = describeError(e)
  } finally {
    quickLogBusy.value = false
  }
}

const sections = computed(
  () =>
    [
      { id: 'todo', title: t('today.section.todo'), variant: 'todo', cards: store.todoCards },
      {
        id: 'maintenance',
        title: t('today.section.maintenance'),
        variant: 'maintenance',
        cards: store.maintenanceCards,
      },
      { id: 'quota', title: t('today.section.quota'), variant: 'quota', cards: store.quotaCards },
    ] as const,
)
const visibleCount = computed(
  () =>
    store.overdueCards.length +
    store.todoCards.length +
    store.maintenanceCards.length +
    store.quotaCards.length +
    store.doneCards.length,
)

function cat(c: CardVM) {
  return findCategory(store.categories, c.detail.category_id)
}
const log = (input: CheckinInput) => store.submit(input)

const detailItemId = ref<number | null>(null)
const allCards = computed<CardVM[]>(() => [
  ...store.overdueCards,
  ...store.todoCards,
  ...store.maintenanceCards,
  ...store.quotaCards,
  ...store.doneCards,
])
const detailCard = computed(() =>
  detailItemId.value !== null
    ? (allCards.value.find((c) => c.detail.id === detailItemId.value) ?? null)
    : null,
)

onMounted(store.refresh)
// The store clears all plaintext synchronously when redact turns on; refetch afterwards.
watch(
  () => privacy.redact,
  () => store.refresh(),
)
// Queued offline actions were confirmed: reload the server state.
watch(
  () => offline.syncedTick,
  () => store.refresh(),
)
</script>

<template>
  <section class="mx-auto max-w-2xl space-y-4 p-4">
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
      <CompletionRing :done="store.ring.done" :total="store.ring.total" />

      <CategoryFilter
        v-model="store.filter"
        :categories="store.categories"
        :total="store.categoryCounts.total"
        :counts="store.categoryCounts.counts"
      />

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
          <span class="text-xs text-error/80">{{ t('today.overdueHint') }}</span>
        </div>
        <ul class="space-y-2" data-testid="overdue-list">
          <li v-for="c in store.overdueCards" :key="c.detail.id">
            <ItemCard
              :vm="c"
              :category="cat(c)"
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
          :data-testid="`section-${s.id}`"
        >
          <h2 :id="`h-${s.id}`" class="mb-2 text-base font-semibold text-base-content/90">
            {{ t('common.countSuffix', { title: s.title, count: s.cards.length }) }}
          </h2>
          <ul class="space-y-2" :data-testid="`${s.id}-list`">
            <li v-for="c in s.cards" :key="c.detail.id">
              <ItemCard
                :vm="c"
                :category="cat(c)"
                :variant="s.variant"
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

      <!-- Quick Log for Unscheduled items / 随手记 -->
      <div
        class="card card-border bg-base-100 border-base-300 p-4 shadow-xs space-y-3"
        data-testid="section-unscheduled"
      >
        <div class="flex items-center justify-between">
          <h3 class="text-sm font-semibold text-base-content/80 flex items-center gap-2">
            <Icon icon="lucide:pencil" class="size-4 text-primary" />
            {{ t('today.unscheduledTitle') }}
          </h3>
        </div>
        <div class="relative w-full">
          <div class="join w-full">
            <div class="relative flex-1">
              <input
                v-model="quickLogQuery"
                type="text"
                class="input input-sm input-bordered w-full join-item pr-8"
                :placeholder="
                  store.isFuture
                    ? t('today.quickAddFuturePlaceholder')
                    : t('today.quickLogPlaceholder')
                "
                autocomplete="off"
                data-testid="quick-log-input"
                @focus="quickLogOpen = true"
                @blur="onBlurQuickLog"
                @keydown.escape="quickLogOpen = false"
                @keydown.enter.prevent="submitQuickLog({ name: quickLogQuery })"
              />
              <button
                v-if="quickLogQuery"
                type="button"
                class="absolute right-2 top-1.5 text-base-content/40 hover:text-base-content/80"
                @click="quickLogQuery = ''"
              >
                <Icon icon="lucide:x" class="size-4" />
              </button>
            </div>
            <button
              type="button"
              class="btn btn-sm btn-primary join-item shadow-xs"
              :disabled="!quickLogQuery.trim() || quickLogBusy"
              data-testid="quick-log-submit"
              @click="submitQuickLog({ name: quickLogQuery })"
            >
              <span v-if="quickLogBusy" class="loading loading-spinner loading-xs" />
              <Icon v-else icon="lucide:plus" class="size-4" />
              {{ store.isFuture ? t('today.addFutureEvent') : t('today.logNow') }}
            </button>
          </div>

          <!-- Dropdown suggestions -->
          <div
            v-if="
              quickLogOpen &&
              ((!store.isFuture && matchingUnscheduled.length > 0) || quickLogQuery.trim())
            "
            class="absolute left-0 right-0 z-30 mt-1 max-h-60 overflow-y-auto rounded-lg border border-base-300 bg-base-100 p-1 shadow-lg"
          >
            <!-- Create new option -->
            <button
              v-if="
                quickLogQuery.trim() &&
                (store.isFuture ||
                  !matchingUnscheduled.some(
                    (it) => it.name.toLowerCase() === quickLogQuery.trim().toLowerCase(),
                  ))
              "
              type="button"
              class="w-full text-left px-3 py-2 text-sm rounded-md hover:bg-primary/10 text-primary font-medium flex items-center gap-2 cursor-pointer"
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
                @mousedown.prevent="submitQuickLog(u)"
              >
                <span class="font-medium text-base-content/90">{{ u.name }}</span>
                <span class="text-xs text-base-content/50">{{ t('today.card.check') }}</span>
              </button>
            </template>
          </div>
        </div>
      </div>

      <details
        v-if="store.doneCards.length"
        class="card card-border bg-base-100/60 border-base-300/80 shadow-xs group"
        data-testid="section-done"
      >
        <summary
          class="p-3.5 cursor-pointer font-semibold text-base flex items-center justify-between select-none list-none"
        >
          <span class="flex items-center gap-2">
            <Icon icon="lucide:check-circle-2" class="size-4 text-success" aria-hidden="true" />
            {{
              t('common.countSuffix', {
                title: t('today.section.done'),
                count: store.doneCards.length,
              })
            }}
          </span>
          <Icon
            icon="lucide:chevron-down"
            class="size-4 opacity-60 transition-transform duration-200 group-open:rotate-180"
            aria-hidden="true"
          />
        </summary>
        <div class="p-3 pt-0">
          <ul class="space-y-2" data-testid="done-list">
            <li v-for="c in store.doneCards" :key="c.detail.id">
              <ItemCard
                :vm="c"
                :category="cat(c)"
                variant="done"
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
      />
    </template>
  </section>
</template>
