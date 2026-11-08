<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { useOfflineStore } from '../stores/offline'
import { usePrivacyStore } from '../stores/privacy'
import { useTodayStore } from '../stores/today'
import { findCategory, type CardVM } from '../today/logic'
import CompletionRing from '../components/today/CompletionRing.vue'
import CategoryFilter from '../components/today/CategoryFilter.vue'
import ItemCard from '../components/today/ItemCard.vue'
import QuickAddForm from '../components/today/QuickAddForm.vue'
import type { CheckinInput } from '../api'

const WEEKDAY_NAMES = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']

const privacy = usePrivacyStore()
const store = useTodayStore()
const offline = useOfflineStore()

const sections = computed(() =>
  [
    { id: 'todo', title: 'To do', variant: 'todo', cards: store.todoCards },
    { id: 'maintenance', title: 'Maintenance due', variant: 'maintenance', cards: store.maintenanceCards },
    { id: 'quota', title: 'Quota', variant: 'quota', cards: store.quotaCards },
  ] as const,
)
const visibleCount = computed(
  () => store.todoCards.length + store.maintenanceCards.length + store.quotaCards.length + store.doneCards.length,
)

function cat(c: CardVM) {
  return findCategory(store.categories, c.detail.category_id)
}
const log = (input: CheckinInput) => store.submit(input)

onMounted(store.refresh)
// The store clears all plaintext synchronously when redact turns on; refetch afterwards.
watch(() => privacy.redact, () => store.refresh())
// Queued offline actions were confirmed: reload the server state.
watch(() => offline.syncedTick, () => store.refresh())
</script>

<template>
  <section class="mx-auto max-w-2xl space-y-4 p-4">
    <header v-if="store.view" class="flex flex-wrap items-baseline justify-between gap-2">
      <h1 class="text-2xl font-bold">
        Today {{ store.view.date }} <small class="text-base font-normal">{{ WEEKDAY_NAMES[store.view.weekday] ?? '' }}</small>
      </h1>
    </header>

    <p v-if="store.loading && !store.view" role="status" class="py-8 text-center opacity-70" data-testid="loading">
      <span class="loading loading-spinner loading-sm" aria-hidden="true"></span> Loading...
    </p>

    <div v-if="store.error" class="alert alert-error" role="alert" data-testid="error">
      <span>{{ store.error }}</span>
      <button type="button" class="btn btn-sm" data-testid="retry" @click="store.refresh()">Retry</button>
    </div>

    <template v-if="store.view">
      <CompletionRing :done="store.ring.done" :total="store.ring.total" />

      <CategoryFilter
        v-model="store.filter" :categories="store.categories" :total="store.categoryCounts.total"
        :counts="store.categoryCounts.counts"
      />

      <p v-if="store.categoryCounts.total === 0" class="py-6 text-center opacity-70" data-testid="empty">
        Nothing is scheduled for today.
        <RouterLink to="/items" class="link">Manage items</RouterLink>
      </p>
      <p v-else-if="visibleCount === 0" class="py-6 text-center opacity-70" data-testid="empty-filter">
        No items in this category today.
      </p>

      <template v-for="s in sections" :key="s.id">
        <section v-if="s.cards.length" :aria-labelledby="`h-${s.id}`" :data-testid="`section-${s.id}`">
          <h2 :id="`h-${s.id}`" class="mb-2 text-lg font-semibold">{{ s.title }} ({{ s.cards.length }})</h2>
          <ul class="space-y-2" :data-testid="`${s.id}-list`">
            <li v-for="c in s.cards" :key="c.detail.id">
              <ItemCard
                :vm="c" :category="cat(c)" :variant="s.variant" :busy="store.busy.includes(c.detail.id)" :pending="offline.pendingItems.includes(c.detail.id)"
                @log="log" @remove="store.remove(c.detail.id, $event)"
              />
            </li>
          </ul>
        </section>
      </template>

      <details v-if="store.doneCards.length" data-testid="section-done">
        <summary class="mb-2 cursor-pointer text-lg font-semibold">Done ({{ store.doneCards.length }})</summary>
        <ul class="space-y-2" data-testid="done-list">
          <li v-for="c in store.doneCards" :key="c.detail.id">
            <ItemCard
              :vm="c" :category="cat(c)" variant="done" :busy="store.busy.includes(c.detail.id)" :pending="offline.pendingItems.includes(c.detail.id)"
              @log="log" @remove="store.remove(c.detail.id, $event)"
            />
          </li>
        </ul>
      </details>
    </template>

    <QuickAddForm @created="store.refresh()" />
  </section>
</template>
