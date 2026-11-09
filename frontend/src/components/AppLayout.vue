<script setup lang="ts">
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

function isNavActive(path: string) {
  return path === '/' ? route.path === '/' : route.path.startsWith(path)
}
</script>

<template>
  <div class="min-h-screen md:flex" :data-privacy="privacy.redact ? 'on' : 'off'">
    <!-- Desktop sidebar -->
    <aside class="hidden w-60 shrink-0 border-r border-base-300 bg-base-200/50 md:block">
      <div class="sticky top-0 flex flex-col p-4">
        <div class="flex items-center gap-2.5 px-2 py-3 mb-2">
          <div
            class="size-8 rounded-lg bg-primary text-primary-content flex items-center justify-center font-bold shadow-xs"
          >
            <Icon icon="lucide:check-circle-2" class="size-5" aria-hidden="true" />
          </div>
          <span class="text-xl font-bold tracking-tight">Daka</span>
        </div>
        <nav :aria-label="t('nav.main')">
          <ul class="menu w-full gap-1 p-0">
            <li v-for="n in NAV_ITEMS" :key="n.name">
              <RouterLink
                :to="n.path"
                class="flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-colors"
                :class="{
                  'bg-primary text-primary-content font-semibold shadow-xs': isNavActive(n.path),
                }"
                :data-testid="`nav-side-${n.name}`"
              >
                <Icon :icon="n.icon" class="size-4 shrink-0" aria-hidden="true" />
                <span>{{ t(n.labelKey) }}</span>
              </RouterLink>
            </li>
          </ul>
        </nav>
      </div>
    </aside>

    <div class="flex min-w-0 flex-1 flex-col">
      <header
        class="sticky top-0 z-20 flex items-center justify-between gap-2 border-b border-base-300 bg-base-100 px-4 py-2"
      >
        <span class="text-lg font-bold md:invisible">Daka</span>
        <div class="flex items-center gap-2">
          <PrivacyToggle />
          <AvatarMenu />
        </div>
      </header>
      <OfflineBanner />
      <div class="flex-1 pb-20 md:pb-0">
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
