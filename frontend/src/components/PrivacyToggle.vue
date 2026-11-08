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
    class="btn btn-sm"
    :class="privacy.redact ? 'btn-warning' : 'btn-ghost'"
    data-testid="redact-toggle"
    :aria-pressed="privacy.redact"
    :title="t('layout.privacy.title')"
    @click="privacy.toggle()"
  >
    <Icon :icon="privacy.redact ? 'lucide:eye-off' : 'lucide:eye'" aria-hidden="true" />
    <span>{{ privacy.redact ? t('layout.privacy.on') : t('layout.privacy.off') }}</span>
  </button>
</template>
