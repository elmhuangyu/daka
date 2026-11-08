<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, watch } from 'vue'
import { RouterLink, RouterView, useRoute } from 'vue-router'
import { Icon } from '@iconify/vue'
import { usePrivacyStore } from '../stores/privacy'
import { useItemsStore } from '../stores/items'
import { badgesOf, groupItems } from '../items/logic'

const privacy = usePrivacyStore()
const store = useItemsStore()
const route = useRoute()

const groups = computed(() => groupItems(store.items, store.categories, store.filter))
const selectedId = computed(() => (route.params.id ? Number(route.params.id) : 0))
const TONE = { ok: 'badge-success', warn: 'badge-warning', bad: 'badge-error' } as const

let timer: ReturnType<typeof setTimeout> | undefined
function onSearch() {
  clearTimeout(timer)
  timer = setTimeout(() => store.refresh(), 250)
}
onBeforeUnmount(() => clearTimeout(timer))

onMounted(store.refresh)
// The store drops plaintext synchronously when the guard turns on; refetch masked data.
watch(() => privacy.redact, () => store.refresh())
watch(() => store.archived, () => store.refresh())
</script>

<template>
  <div class="mx-auto flex max-w-6xl flex-col gap-4 p-4 lg:flex-row">
    <aside class="w-full shrink-0 space-y-3 lg:w-72" :class="{ 'hidden lg:block': selectedId }" data-testid="panel">
      <div class="flex items-center justify-between">
        <h1 class="text-2xl font-bold">Items</h1>
        <RouterLink to="/items/new" class="btn btn-primary btn-sm" data-testid="new-item">
          <Icon icon="lucide:plus" aria-hidden="true" /> New
        </RouterLink>
      </div>

      <label class="input input-bordered flex items-center gap-2">
        <Icon icon="lucide:search" aria-hidden="true" />
        <input
          v-model="store.q" type="search" class="grow" data-testid="search" aria-label="Search items"
          :placeholder="privacy.redact ? 'Search is off while hidden' : 'Search items'" :disabled="privacy.redact"
          @input="onSearch"
        />
      </label>

      <div class="flex flex-wrap items-center gap-2">
        <select v-model="store.filter" class="select select-bordered select-sm" data-testid="category-filter" aria-label="Category">
          <option value="all">All categories</option>
          <option value="none">Uncategorized</option>
          <option v-for="c in store.categories" :key="c.id" :value="c.id">{{ c.name }}</option>
        </select>
        <label class="label cursor-pointer gap-2 text-sm">
          <input v-model="store.archived" type="checkbox" class="toggle toggle-sm" data-testid="archived-toggle" />
          Archived
        </label>
      </div>

      <div v-if="store.error" class="alert alert-error" role="alert" data-testid="panel-error">
        <span>{{ store.error }}</span>
        <button type="button" class="btn btn-sm" @click="store.refresh()">Retry</button>
      </div>
      <p v-if="store.loading && !store.items.length" role="status" class="opacity-70">Loading...</p>
      <p v-else-if="!store.items.length && !store.error" class="opacity-70" data-testid="panel-empty">No items.</p>

      <section v-for="g in groups" :key="g.key" :data-testid="`group-${g.key}`">
        <h2 class="mb-1 flex items-center gap-1 text-xs font-semibold uppercase opacity-70">
          <Icon v-if="g.sensitive" icon="lucide:lock" aria-hidden="true" />
          {{ g.title }} ({{ g.items.length }})
        </h2>
        <ul class="menu menu-sm w-full p-0">
          <li v-for="it in g.items" :key="it.id" class="flex-row items-center">
            <RouterLink
              :to="`/items/${it.id}`" class="flex min-w-0 flex-1 items-center gap-2" :class="{ 'menu-active': it.id === selectedId }"
              :data-testid="`row-${it.id}`"
            >
              <span class="truncate">{{ it.name }}</span>
              <span
                v-for="b in badgesOf(it)" :key="b.text" class="badge badge-sm" :class="TONE[b.tone]" data-testid="badge"
              >{{ b.text }}</span>
            </RouterLink>
            <button
              type="button" class="btn btn-ghost btn-xs" :class="{ 'text-primary': it.pinned }"
              :aria-label="it.pinned ? 'Unpin' : 'Pin'" :aria-pressed="!!it.pinned" :data-testid="`pin-${it.id}`"
              @click="store.setPinned(it.id, !it.pinned)"
            >
              <Icon icon="lucide:pin" aria-hidden="true" />
            </button>
          </li>
        </ul>
      </section>
    </aside>

    <div class="min-w-0 flex-1"><RouterView /></div>
  </div>
</template>
