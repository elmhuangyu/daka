<script setup lang="ts">
import { useI18n } from '../i18n'

const { t } = useI18n()
</script>

<template>
  <section class="hidden p-6 opacity-70 lg:block" data-testid="items-empty">
    <p>{{ t('items.empty') }}</p>
  </section>
</template>
