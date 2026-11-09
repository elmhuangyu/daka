<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { Icon } from '@iconify/vue'
import { useOfflineStore } from '../stores/offline'
import { usePrivacyStore } from '../stores/privacy'
import { useTodayStore } from '../stores/today'
import { findCategory, type CardVM } from '../today/logic'
import CompletionRing from '../components/today/CompletionRing.vue'
import CategoryFilter from '../components/today/CategoryFilter.vue'
import ItemCard from '../components/today/ItemCard.vue'
import QuickAddForm from '../components/today/QuickAddForm.vue'
import type { CheckinInput } from '../api'
import { useI18n } from '../i18n'
import { weekdayName } from '../i18n/format'

const { t } = useI18n()

const privacy = usePrivacyStore()
const store = useTodayStore()
const offline = useOfflineStore()

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
    store.todoCards.length +
    store.maintenanceCards.length +
    store.quotaCards.length +
    store.doneCards.length,
)

function cat(c: CardVM) {
  return findCategory(store.categories, c.detail.category_id)
}
const log = (input: CheckinInput) => store.submit(input)

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
    <header v-if="store.view" class="flex flex-wrap items-baseline justify-between gap-2 pb-1">
      <div>
        <h1 class="text-2xl font-bold tracking-tight">
          {{ t('today.heading') }} {{ store.view.date }}
          <small class="text-base font-normal text-base-content/70 ml-1">{{
            weekdayName(store.view.weekday)
          }}</small>
        </h1>
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
              />
            </li>
          </ul>
        </section>
      </template>

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
              />
            </li>
          </ul>
        </div>
      </details>
    </template>

    <QuickAddForm @created="store.refresh()" />
  </section>
</template>
