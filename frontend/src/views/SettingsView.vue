<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Icon } from '@iconify/vue'
import GeneralSection from '../components/settings/GeneralSection.vue'
import CategoriesSection from '../components/settings/CategoriesSection.vue'
import TokensSection from '../components/settings/TokensSection.vue'
import ExternalCalendarsSection from '../components/settings/ExternalCalendarsSection.vue'
import TelegramSection from '../components/settings/TelegramSection.vue'
import DataSection from '../components/settings/DataSection.vue'
import PrivacyAccountSections from '../components/settings/PrivacyAccountSections.vue'
import { useI18n, type MessageKey } from '../i18n'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

type TabKey = 'general' | 'categories' | 'integrations' | 'privacy' | 'data'

const TABS: { id: TabKey; labelKey: MessageKey; icon: string }[] = [
  { id: 'general', labelKey: 'settings.tabs.general', icon: 'lucide:sliders' },
  { id: 'categories', labelKey: 'settings.tabs.categories', icon: 'lucide:tags' },
  { id: 'integrations', labelKey: 'settings.tabs.integrations', icon: 'lucide:link-2' },
  { id: 'privacy', labelKey: 'settings.tabs.privacyAccount', icon: 'lucide:shield' },
  { id: 'data', labelKey: 'settings.tabs.data', icon: 'lucide:database' },
]

const validTab = (k: string): k is TabKey => TABS.some((tab) => tab.id === k)

const activeTab = ref<TabKey>('general')

function setTab(id: TabKey) {
  activeTab.value = id
  void router.replace({ query: { ...route.query, tab: id } })
}

onMounted(() => {
  const q = route.query.tab
  activeTab.value = typeof q === 'string' && validTab(q) ? q : 'general'
})

watch(
  () => route.query.tab,
  (q) => {
    activeTab.value = typeof q === 'string' && validTab(q) ? q : 'general'
  },
)
</script>

<template>
  <div class="mx-auto max-w-6xl p-4 lg:p-6 space-y-6">
    <div class="flex flex-col gap-1">
      <h1 class="text-2xl font-bold tracking-tight">{{ t('settings.title') }}</h1>
    </div>

    <!-- Mobile horizontal navigation -->
    <nav
      class="flex lg:hidden overflow-x-auto gap-1.5 pb-2 -mx-4 px-4 scrollbar-none"
      :aria-label="t('nav.settings')"
    >
      <button
        v-for="tb in TABS"
        :key="tb.id"
        :data-testid="`tab-${tb.id}`"
        class="btn btn-sm shrink-0 gap-1.5 rounded-lg border font-medium transition-all"
        :class="
          activeTab === tb.id
            ? 'btn-primary shadow-sm'
            : 'btn-ghost bg-base-200/60 border-base-300 text-base-content/80'
        "
        @click="setTab(tb.id)"
      >
        <Icon :icon="tb.icon" class="size-4" aria-hidden="true" />
        <span>{{ t(tb.labelKey) }}</span>
      </button>
    </nav>

    <!-- Main settings container -->
    <div class="flex flex-col lg:flex-row gap-6 items-start">
      <!-- Desktop sidebar tabs -->
      <aside class="hidden lg:block w-60 shrink-0 sticky top-20">
        <nav
          class="flex flex-col gap-1 p-1.5 rounded-2xl bg-base-200/50 border border-base-300 shadow-sm"
          :aria-label="t('nav.settings')"
        >
          <button
            v-for="tb in TABS"
            :key="tb.id"
            :data-testid="`tab-side-${tb.id}`"
            class="flex items-center gap-3 px-3.5 py-2.5 rounded-xl text-sm font-medium transition-colors text-left"
            :class="
              activeTab === tb.id
                ? 'bg-primary text-primary-content font-semibold shadow-sm'
                : 'text-base-content/80 hover:bg-base-200 hover:text-base-content'
            "
            @click="setTab(tb.id)"
          >
            <Icon :icon="tb.icon" class="size-4.5 shrink-0" aria-hidden="true" />
            <span class="truncate">{{ t(tb.labelKey) }}</span>
          </button>
        </nav>
      </aside>

      <!-- Active content panels -->
      <main class="flex-1 min-w-0 w-full space-y-6">
        <GeneralSection v-show="activeTab === 'general'" />

        <CategoriesSection v-show="activeTab === 'categories'" />

        <div v-show="activeTab === 'integrations'" class="space-y-6">
          <TokensSection />
          <ExternalCalendarsSection />
          <TelegramSection />
        </div>

        <div v-show="activeTab === 'privacy'">
          <PrivacyAccountSections />
        </div>

        <DataSection v-show="activeTab === 'data'" />
      </main>
    </div>
  </div>
</template>
