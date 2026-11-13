<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, RouterView, useRoute } from 'vue-router'
import { Icon } from '@iconify/vue'
import { usePrivacyStore } from '../stores/privacy'
import { useItemsStore } from '../stores/items'
import { badgesOf, groupItems } from '../items/logic'
import { isModalOpen, isTypingTarget } from '../composables/useShortcuts'
import { useI18n } from '../i18n'

const { t } = useI18n()

const privacy = usePrivacyStore()
const store = useItemsStore()
const route = useRoute()
const panelEl = ref<HTMLElement | null>(null)

function onKeydown(e: KeyboardEvent) {
  if (e.defaultPrevented || e.isComposing || e.ctrlKey || e.altKey || e.metaKey || e.shiftKey)
    return
  if (e.key !== 'ArrowUp' && e.key !== 'ArrowDown') return
  if (isTypingTarget(e.target) || isModalOpen()) return
  const rows = [...(panelEl.value?.querySelectorAll<HTMLElement>('[data-testid^="row-"]') ?? [])]
  if (!rows.length) return
  e.preventDefault()
  const dir = e.key === 'ArrowDown' ? 1 : -1
  const cur = document.activeElement?.closest<HTMLElement>('[data-testid^="row-"]') ?? null
  const i = cur ? rows.indexOf(cur) : -1
  const next =
    i < 0 ? (dir === 1 ? 0 : rows.length - 1) : Math.min(rows.length - 1, Math.max(0, i + dir))
  const el = rows[next]!
  if (el !== document.activeElement) {
    el.focus()
    el.scrollIntoView?.({ block: 'nearest' })
  }
}

const groups = computed(() => groupItems(store.items, store.categories, store.filter))
const selectedId = computed(() => (route.params.id ? Number(route.params.id) : 0))
const TONE = { ok: 'badge-success', warn: 'badge-warning', bad: 'badge-error' } as const

let timer: ReturnType<typeof setTimeout> | undefined
function onSearch() {
  clearTimeout(timer)
  timer = setTimeout(() => store.refresh(), 250)
}
onBeforeUnmount(() => {
  clearTimeout(timer)
  window.removeEventListener('keydown', onKeydown)
})

onMounted(() => {
  void store.refresh()
  window.addEventListener('keydown', onKeydown)
})
// The store drops plaintext synchronously when the guard turns on; refetch masked data.
watch(
  () => privacy.redact,
  () => store.refresh(),
)
watch(
  () => store.archived,
  () => store.refresh(),
)
</script>

<template>
  <div class="mx-auto flex max-w-6xl flex-col gap-6 p-4 lg:flex-row">
    <aside
      ref="panelEl"
      class="w-full shrink-0 lg:w-80"
      :class="{ 'hidden lg:block': selectedId }"
      data-testid="panel"
    >
      <div class="card card-border bg-base-100 border-base-300 p-4 shadow-sm space-y-3.5">
        <div class="flex items-center justify-between pb-1">
          <h1 class="text-2xl font-bold tracking-tight">{{ t('items.title') }}</h1>
          <RouterLink
            to="/items/new"
            class="btn btn-primary btn-sm shadow-xs"
            data-testid="new-item"
          >
            <Icon icon="lucide:plus" aria-hidden="true" /> {{ t('items.new') }}
          </RouterLink>
        </div>

        <label class="input input-sm input-bordered flex items-center gap-2">
          <Icon icon="lucide:search" class="size-4 opacity-60" aria-hidden="true" />
          <input
            v-model="store.q"
            type="search"
            class="grow"
            data-testid="search"
            data-shortcut-search
            :aria-label="t('items.search')"
            :placeholder="privacy.redact ? t('items.searchOff') : t('items.search')"
            :disabled="privacy.redact"
            @input="onSearch"
          />
        </label>

        <div class="flex flex-wrap items-center justify-between gap-2">
          <select
            v-model="store.filter"
            class="select select-bordered select-sm flex-1 min-w-32"
            data-testid="category-filter"
            :aria-label="t('items.categoryAria')"
          >
            <option value="all">{{ t('items.allCategories') }}</option>
            <option value="none">{{ t('items.uncategorized') }}</option>
            <option v-for="c in store.categories" :key="c.id" :value="c.id">{{ c.name }}</option>
          </select>
          <label class="label cursor-pointer gap-2 text-xs py-0">
            <input
              v-model="store.archived"
              type="checkbox"
              class="toggle toggle-xs toggle-primary"
              data-testid="archived-toggle"
            />
            <span class="text-base-content/80 font-medium">{{ t('items.archived') }}</span>
          </label>
        </div>

        <div
          v-if="store.error"
          class="alert alert-error text-xs py-2"
          role="alert"
          data-testid="panel-error"
        >
          <span>{{ store.error }}</span>
          <button type="button" class="btn btn-xs" @click="store.refresh()">
            {{ t('common.retry') }}
          </button>
        </div>
        <p
          v-if="store.loading && !store.items.length"
          role="status"
          class="py-4 text-center opacity-70 text-sm"
        >
          <span class="loading loading-spinner loading-xs" aria-hidden="true"></span>
          {{ t('common.loading') }}
        </p>
        <p
          v-else-if="!store.items.length && !store.error"
          class="py-6 text-center opacity-70 text-sm"
          data-testid="panel-empty"
        >
          {{ t('items.none') }}
        </p>

        <div v-else class="space-y-3 pt-1">
          <section v-for="g in groups" :key="g.key" :data-testid="`group-${g.key}`">
            <h2
              class="mb-1.5 px-2 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wider text-base-content/60"
            >
              <Icon
                v-if="g.sensitive"
                icon="lucide:lock"
                class="size-3 text-warning"
                aria-hidden="true"
              />
              <Icon
                v-else-if="g.key === 'pinned'"
                icon="lucide:pin"
                class="size-3 text-primary"
                aria-hidden="true"
              />
              {{ t('common.countSuffix', { title: g.title, count: g.items.length }) }}
            </h2>
            <ul class="menu menu-sm w-full p-0 gap-0.5">
              <li v-for="it in g.items" :key="it.id" class="flex-row items-center">
                <RouterLink
                  :to="`/items/${it.id}`"
                  class="flex min-w-0 flex-1 items-center gap-2 rounded-lg py-2"
                  :class="{ 'menu-active font-medium': it.id === selectedId }"
                  :data-testid="`row-${it.id}`"
                >
                  <span
                    class="size-2 rounded-full shrink-0"
                    :class="{ 'border border-base-content': !it.color }"
                    :style="it.color ? { backgroundColor: it.color } : {}"
                  />
                  <span v-if="it.emoji" aria-hidden="true" class="shrink-0">{{ it.emoji }}</span>
                  <span class="truncate">{{ it.name }}</span>
                  <span
                    v-for="b in badgesOf(it)"
                    :key="b.text"
                    class="badge badge-xs ml-auto shrink-0"
                    :class="TONE[b.tone]"
                    data-testid="badge"
                    >{{ b.text }}</span
                  >
                </RouterLink>
                <button
                  type="button"
                  class="btn btn-ghost btn-xs btn-square shrink-0 ml-1"
                  :class="{ 'text-primary': it.pinned }"
                  :aria-label="it.pinned ? t('items.unpin') : t('items.pin')"
                  :aria-pressed="!!it.pinned"
                  :data-testid="`pin-${it.id}`"
                  @click="store.setPinned(it.id, !it.pinned)"
                >
                  <Icon icon="lucide:pin" class="size-3.5" aria-hidden="true" />
                </button>
              </li>
            </ul>
          </section>
        </div>
      </div>
    </aside>

    <div class="min-w-0 flex-1"><RouterView /></div>
  </div>
</template>
