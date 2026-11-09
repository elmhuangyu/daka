<script setup lang="ts">
import { Icon } from '@iconify/vue'
import { RouterLink } from 'vue-router'
import { useI18n } from '../i18n'

const { t } = useI18n()
</script>

<template>
  <section
    class="hidden h-full min-h-[380px] lg:flex items-center justify-center p-8"
    data-testid="items-empty"
  >
    <div
      class="card card-border bg-base-100 border-base-300 max-w-sm p-8 text-center items-center gap-3 shadow-xs"
    >
      <div
        class="size-14 rounded-2xl bg-base-200 flex items-center justify-center text-primary/80 mb-1"
      >
        <Icon icon="lucide:list-checks" class="size-7" aria-hidden="true" />
      </div>
      <p class="text-sm text-base-content/70">
        {{ t('items.empty') }}
      </p>
      <RouterLink to="/items/new" class="btn btn-primary btn-sm mt-2 gap-1.5 shadow-xs">
        <Icon icon="lucide:plus" class="size-4" aria-hidden="true" />
        {{ t('items.new') }}
      </RouterLink>
    </div>
  </section>
</template>
