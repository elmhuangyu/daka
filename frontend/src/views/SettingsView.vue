<script setup lang="ts">
import GeneralSection from '../components/settings/GeneralSection.vue'
import CategoriesSection from '../components/settings/CategoriesSection.vue'
import TokensSection from '../components/settings/TokensSection.vue'
import ExternalCalendarsSection from '../components/settings/ExternalCalendarsSection.vue'
import TelegramSection from '../components/settings/TelegramSection.vue'
import DataSection from '../components/settings/DataSection.vue'
import PrivacyAccountSections from '../components/settings/PrivacyAccountSections.vue'
import { useI18n } from '../i18n'

const { t } = useI18n()
</script>

<template>
  <div class="mx-auto flex max-w-2xl flex-col gap-4 p-4">
    <h1 class="text-2xl font-bold">{{ t('settings.title') }}</h1>
    <GeneralSection />
    <CategoriesSection />
    <TokensSection />
    <ExternalCalendarsSection />
    <TelegramSection />
    <PrivacyAccountSections />
    <DataSection />
  </div>
</template>
