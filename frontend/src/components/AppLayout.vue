<script setup lang="ts">
import { computed, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { Icon } from '@iconify/vue'
import { NAV_ITEMS } from '../nav'
import { usePrivacyStore } from '../stores/privacy'
import { useTodayStore } from '../stores/today'
import { useCalendarStore } from '../stores/calendar'
import { useProjectsStore } from '../stores/projects'
import { useItemDetailStore, useItemsStore } from '../stores/items'
import { helpOpen } from '../composables/useGlobalShortcuts'
import { periodTitle } from '../calendar/logic'
import { formatCustomDate, weekdayName } from '../i18n/format'
import OfflineBanner from './OfflineBanner.vue'
import AvatarMenu from './AvatarMenu.vue'
import PrivacyToggle from './PrivacyToggle.vue'
import { useI18n, type MessageKey } from '../i18n'

const { t } = useI18n()
const privacy = usePrivacyStore()
const todayStore = useTodayStore()
const calendarStore = useCalendarStore()
const projectsStore = useProjectsStore()
const itemsStore = useItemsStore()
const itemDetailStore = useItemDetailStore()
const route = useRoute()

const SETTINGS_TAB_LABELS: Record<string, MessageKey> = {
  general: 'settings.tabs.general',
  categories: 'settings.tabs.categories',
  integrations: 'settings.tabs.integrations',
  privacy: 'settings.tabs.privacyAccount',
  data: 'settings.tabs.data',
  shortcuts: 'shortcuts.tab',
}

function readCollapsed(): boolean {
  try {
    return (
      typeof localStorage !== 'undefined' &&
      localStorage.getItem('daka:sidebar_collapsed') === 'true'
    )
  } catch {
    return false
  }
}

const collapsed = ref(readCollapsed())

function toggleSidebar() {
  collapsed.value = !collapsed.value
  try {
    localStorage.setItem('daka:sidebar_collapsed', String(collapsed.value))
  } catch {
    // localStorage may be unavailable
  }
}

function isNavActive(path: string) {
  return path === '/' ? route.path === '/' : route.path.startsWith(path)
}

const activeNav = computed(
  () =>
    NAV_ITEMS.find((n) => (n.path === '/' ? route.path === '/' : route.path.startsWith(n.path))) ??
    NAV_ITEMS[0]!,
)

const subCrumb = computed<string | null>(() => {
  switch (route.name) {
    case 'project-detail': {
      const id = Number(route.params.id)
      const proj = projectsStore.projectById.get(id)
      if (!proj) return null
      return proj.emoji ? `${proj.emoji} ${proj.name}` : proj.name
    }
    case 'item-new':
      return t('items.editor.titleNew')
    case 'item-edit':
      return t('items.editor.titleEdit')
    case 'item-detail': {
      const id = Number(route.params.id)
      if (itemDetailStore.item?.id === id) return itemDetailStore.item.name
      return itemsStore.items.find((it) => it.id === id)?.name ?? null
    }
    case 'settings': {
      const tab = typeof route.query.tab === 'string' ? route.query.tab : 'general'
      const key = SETTINGS_TAB_LABELS[tab] ?? SETTINGS_TAB_LABELS.general
      return t(key)
    }
    default:
      return null
  }
})

const todayDone = computed(() => todayStore.ring.done + todayStore.todoRing.done)
const todayTotal = computed(() => todayStore.ring.total + todayStore.todoRing.total)
const todayPct = computed(() =>
  todayTotal.value > 0 ? Math.round((todayDone.value / todayTotal.value) * 100) : 0,
)
</script>

<template>
  <div
    class="h-full flex flex-col md:flex-row overflow-hidden"
    :data-privacy="privacy.redact ? 'on' : 'off'"
  >
    <!-- Desktop sidebar -->
    <aside
      class="relative z-10 hidden shrink-0 border-r border-base-300/60 bg-base-200/45 backdrop-blur-md md:flex flex-col h-full overflow-y-auto transition-[width] duration-200"
      :class="collapsed ? 'w-16' : 'w-60'"
    >
      <div
        class="h-14 shrink-0 border-b border-base-300/60 flex items-center gap-2.5"
        :class="collapsed ? 'justify-center px-2' : 'px-3'"
      >
        <!-- Collapsed: brand logo is the expand trigger; hover shows chevron-right -->
        <button
          v-if="collapsed"
          type="button"
          class="relative group size-9 rounded-lg bg-primary text-primary-content flex items-center justify-center font-bold shadow-xs cursor-pointer border-0 p-0 transition-transform hover:scale-105"
          :title="t('nav.expand')"
          :aria-label="t('nav.expand')"
          data-testid="sidebar-toggle"
          @click="toggleSidebar"
        >
          <Icon
            icon="lucide:check-circle-2"
            class="size-5 transition-opacity group-hover:opacity-0"
            aria-hidden="true"
          />
          <Icon
            icon="lucide:chevron-right"
            class="size-5 absolute inset-0 m-auto opacity-0 group-hover:opacity-100 transition-opacity"
            aria-hidden="true"
          />
        </button>

        <!-- Expanded: brand logo + text + collapse button -->
        <template v-else>
          <div
            class="size-8 rounded-lg bg-primary text-primary-content flex items-center justify-center font-bold shadow-xs shrink-0"
          >
            <Icon icon="lucide:check-circle-2" class="size-5" aria-hidden="true" />
          </div>
          <span class="text-xl font-bold tracking-tight truncate">Daka</span>
          <button
            type="button"
            class="btn btn-ghost btn-xs btn-circle ml-auto shrink-0"
            :title="t('nav.collapse')"
            :aria-label="t('nav.collapse')"
            data-testid="sidebar-toggle"
            @click="toggleSidebar"
          >
            <Icon icon="lucide:chevron-left" class="size-4" />
          </button>
        </template>
      </div>
      <div class="flex flex-col p-3 flex-1">
        <nav data-nav :aria-label="t('nav.main')">
          <ul class="menu w-full gap-1 p-0">
            <li v-for="n in NAV_ITEMS" :key="n.name">
              <RouterLink
                :to="n.path"
                class="flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-colors"
                :class="{
                  'justify-center px-0': collapsed,
                  'bg-primary text-primary-content font-semibold shadow-xs': isNavActive(n.path),
                }"
                :title="t(n.labelKey)"
                :aria-label="t(n.labelKey)"
                :data-testid="`nav-side-${n.name}`"
              >
                <Icon :icon="n.icon" class="size-4 shrink-0" aria-hidden="true" />
                <span v-if="!collapsed">{{ t(n.labelKey) }}</span>
              </RouterLink>
            </li>
          </ul>
        </nav>
      </div>
    </aside>

    <div class="flex min-w-0 flex-1 flex-col h-full overflow-hidden">
      <header
        class="shrink-0 z-20 h-14 flex items-center justify-between gap-3 border-b border-base-300/60 bg-base-100/25 backdrop-blur-md px-4 sm:px-6"
      >
        <div class="flex items-center gap-2.5 min-w-0">
          <!-- Mobile brand + section -->
          <div class="flex md:hidden items-center gap-2 min-w-0">
            <div
              class="size-7 rounded-lg bg-primary text-primary-content flex items-center justify-center shadow-2xs shrink-0"
            >
              <Icon icon="lucide:check-circle-2" class="size-4" aria-hidden="true" />
            </div>
            <span class="text-base font-bold tracking-tight shrink-0">Daka</span>
            <span class="text-base-content/30 shrink-0" aria-hidden="true">/</span>
            <span class="text-sm font-medium text-base-content/75 truncate">
              {{ t(activeNav.labelKey) }}
            </span>
          </div>

          <!-- Desktop breadcrumb + live context -->
          <div class="hidden md:flex items-center gap-2.5 min-w-0">
            <div
              class="size-7 rounded-lg bg-primary/12 text-primary flex items-center justify-center shrink-0"
            >
              <Icon :icon="activeNav.icon" class="size-4" aria-hidden="true" />
            </div>
            <RouterLink
              v-if="subCrumb"
              :to="activeNav.path"
              class="text-sm font-medium text-base-content/70 hover:text-base-content transition-colors shrink-0"
            >
              {{ t(activeNav.labelKey) }}
            </RouterLink>
            <span v-else class="text-sm font-semibold text-base-content shrink-0">
              {{ t(activeNav.labelKey) }}
            </span>
            <template v-if="subCrumb">
              <Icon
                icon="lucide:chevron-right"
                class="size-3.5 text-base-content/40 shrink-0"
                aria-hidden="true"
              />
              <span
                class="text-sm font-semibold text-base-content truncate"
                data-testid="topbar-subcrumb"
              >
                {{ subCrumb }}
              </span>
            </template>

            <template v-if="route.name === 'today' && todayStore.view">
              <span class="h-4 w-px bg-base-300/80 shrink-0" aria-hidden="true"></span>
              <span
                class="inline-flex items-center gap-1.5 text-xs text-base-content/70 font-medium tabular-nums shrink-0"
                data-testid="topbar-today-date"
              >
                <Icon
                  icon="lucide:calendar"
                  class="size-3.5 text-base-content/50 shrink-0"
                  aria-hidden="true"
                />
                <span class="font-mono">{{ formatCustomDate(todayStore.view.date) }}</span>
                <span aria-hidden="true">·</span>
                <span>{{ weekdayName(todayStore.view.weekday) }}</span>
              </span>
              <span
                v-if="todayTotal > 0"
                class="inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-semibold tabular-nums shrink-0 transition-colors"
                :class="
                  todayPct >= 100 ? 'bg-success/15 text-success' : 'bg-primary/12 text-primary'
                "
                data-testid="topbar-today-progress"
              >
                <span
                  class="size-1.5 rounded-full shrink-0"
                  :class="todayPct >= 100 ? 'bg-success' : 'bg-primary'"
                  aria-hidden="true"
                ></span>
                <span>{{ todayDone }}/{{ todayTotal }} · {{ todayPct }}%</span>
              </span>
            </template>

            <template v-else-if="route.name === 'calendar' && calendarStore.anchor">
              <span class="h-4 w-px bg-base-300/80 shrink-0" aria-hidden="true"></span>
              <span
                class="inline-flex items-center gap-1.5 text-xs text-base-content/70 font-medium shrink-0"
                data-testid="topbar-calendar-period"
              >
                <Icon
                  icon="lucide:calendar-days"
                  class="size-3.5 text-base-content/50 shrink-0"
                  aria-hidden="true"
                />
                <span>{{
                  periodTitle(calendarStore.mode, calendarStore.anchor, calendarStore.weekStart)
                }}</span>
              </span>
            </template>
          </div>
        </div>

        <div class="flex items-center gap-2 shrink-0">
          <button
            type="button"
            class="btn btn-sm rounded-full border border-base-300/70 bg-base-200/40 hover:bg-base-200/80 text-xs font-medium gap-1.5 px-2.5 shadow-2xs"
            :title="t('shortcuts.title')"
            :aria-label="t('shortcuts.title')"
            data-testid="topbar-shortcuts"
            @click="helpOpen = true"
          >
            <Icon icon="lucide:keyboard" class="size-3.5" aria-hidden="true" />
            <kbd class="kbd kbd-xs hidden sm:inline-flex">?</kbd>
          </button>
          <PrivacyToggle />
          <div class="h-5 w-px bg-base-300/70 hidden sm:block" aria-hidden="true"></div>
          <AvatarMenu />
        </div>
      </header>
      <OfflineBanner />
      <div class="flex-1 overflow-y-auto pb-20 md:pb-6" data-testid="main-content">
        <slot />
      </div>
    </div>

    <!-- Mobile bottom bar -->
    <nav data-nav class="dock md:hidden" :aria-label="t('nav.main')">
      <RouterLink
        v-for="n in NAV_ITEMS"
        :key="n.name"
        :to="n.path"
        :data-testid="`nav-bottom-${n.name}`"
      >
        <Icon :icon="n.icon" aria-hidden="true" class="size-[1.2em]" />
        <span class="dock-label">{{ t(n.labelKey) }}</span>
      </RouterLink>
    </nav>
  </div>
</template>
