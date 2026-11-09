<script setup lang="ts">
import { ref } from 'vue'
import { useRoute } from 'vue-router'
import { Icon } from '@iconify/vue'
import { NAV_ITEMS } from '../nav'
import { usePrivacyStore } from '../stores/privacy'
import OfflineBanner from './OfflineBanner.vue'
import AvatarMenu from './AvatarMenu.vue'
import PrivacyToggle from './PrivacyToggle.vue'
import { useI18n } from '../i18n'

const { t } = useI18n()
const privacy = usePrivacyStore()
const route = useRoute()

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
</script>

<template>
  <div
    class="h-full flex flex-col md:flex-row overflow-hidden"
    :data-privacy="privacy.redact ? 'on' : 'off'"
  >
    <!-- Desktop sidebar -->
    <aside
      class="hidden shrink-0 border-r border-base-300 bg-base-200/50 md:flex flex-col h-full overflow-y-auto transition-[width] duration-200"
      :class="collapsed ? 'w-16' : 'w-60'"
    >
      <div class="flex flex-col p-3 flex-1">
        <div
          class="flex items-center gap-2 px-1 py-3 mb-2"
          :class="{ 'justify-center': collapsed }"
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
        <nav :aria-label="t('nav.main')">
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
        class="shrink-0 z-20 flex items-center justify-between gap-2 border-b border-base-300 bg-base-100 px-4 py-2"
      >
        <div class="flex items-center gap-2">
          <span class="text-lg font-bold md:invisible">Daka</span>
        </div>
        <div class="flex items-center gap-2">
          <PrivacyToggle />
          <AvatarMenu />
        </div>
      </header>
      <OfflineBanner />
      <div class="flex-1 overflow-y-auto pb-20 md:pb-6" data-testid="main-content">
        <slot />
      </div>
    </div>

    <!-- Mobile bottom bar -->
    <nav class="dock md:hidden" :aria-label="t('nav.main')">
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
