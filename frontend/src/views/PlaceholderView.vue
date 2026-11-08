<script setup lang="ts">
import { useRoute } from 'vue-router'
import { useI18n } from '../i18n'

const { t } = useI18n()

const route = useRoute()
</script>

<template>
  <section class="mx-auto max-w-2xl p-4">
    <h1 class="text-2xl font-bold">{{ route.meta.title }}</h1>
    <p class="mt-2 opacity-70">{{ t('placeholder.comingSoon') }}</p>
  </section>
</template>
