<script setup lang="ts">
import { Icon } from '@iconify/vue'
import { NAV_ITEMS } from '../nav'
import { usePrivacyStore } from '../stores/privacy'
import OfflineBanner from './OfflineBanner.vue'
import AvatarMenu from './AvatarMenu.vue'
import PrivacyToggle from './PrivacyToggle.vue'
import { useI18n } from '../i18n'

const { t } = useI18n()
const privacy = usePrivacyStore()
</script>

<template>
  <div class="min-h-screen md:flex" :data-privacy="privacy.redact ? 'on' : 'off'">
    <!-- Desktop sidebar -->
    <aside class="hidden w-56 shrink-0 border-r border-base-300 bg-base-200 md:block">
      <div class="sticky top-0 p-3">
        <p class="px-3 py-2 text-xl font-bold">Daka</p>
        <nav :aria-label="t('nav.main')">
          <ul class="menu w-full gap-1">
            <li v-for="n in NAV_ITEMS" :key="n.name">
              <RouterLink :to="n.path" :data-testid="`nav-side-${n.name}`">
                <Icon :icon="n.icon" aria-hidden="true" /> {{ t(n.labelKey) }}
              </RouterLink>
            </li>
          </ul>
        </nav>
      </div>
    </aside>

    <div class="flex min-w-0 flex-1 flex-col">
      <header class="sticky top-0 z-20 flex items-center justify-between gap-2 border-b border-base-300 bg-base-100 px-4 py-2">
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
      <RouterLink v-for="n in NAV_ITEMS" :key="n.name" :to="n.path" :data-testid="`nav-bottom-${n.name}`">
        <Icon :icon="n.icon" aria-hidden="true" class="size-[1.2em]" />
        <span class="dock-label">{{ t(n.labelKey) }}</span>
      </RouterLink>
    </nav>
  </div>
</template>
