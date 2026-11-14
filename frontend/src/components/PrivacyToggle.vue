<script setup lang="ts">
import { Icon } from '@iconify/vue'
import { usePrivacyStore } from '../stores/privacy'
import { useI18n } from '../i18n'

const { t } = useI18n()

const privacy = usePrivacyStore()
</script>

<template>
  <button
    type="button"
    class="btn btn-sm rounded-full text-xs gap-1.5 px-3"
    :class="
      privacy.redact
        ? 'btn-warning font-semibold shadow-xs'
        : 'border border-base-300/70 bg-base-200/40 hover:bg-base-200/80 font-medium shadow-2xs'
    "
    data-testid="redact-toggle"
    :aria-pressed="privacy.redact"
    :title="t('layout.privacy.title')"
    @click="privacy.toggle()"
  >
    <Icon
      :icon="privacy.redact ? 'lucide:eye-off' : 'lucide:eye'"
      class="size-3.5"
      aria-hidden="true"
    />
    <span>{{ privacy.redact ? t('layout.privacy.on') : t('layout.privacy.off') }}</span>
  </button>
</template>
