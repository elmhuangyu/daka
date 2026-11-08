<script setup lang="ts">
import { computed } from 'vue'
import { percent } from '../../today/logic'
import { useI18n } from '../../i18n'

const { t } = useI18n()

const props = defineProps<{ done: number; total: number }>()
const pct = computed(() => percent(props.done, props.total))
const R = 42
const C = 2 * Math.PI * R
const dash = computed(() => `${(pct.value / 100) * C} ${C}`)
</script>

<template>
  <div class="flex items-center gap-4" data-testid="ring">
    <div
      class="relative h-24 w-24 shrink-0"
      role="progressbar"
      :aria-label="t('today.ring.label')"
      aria-valuemin="0"
      aria-valuemax="100"
      :aria-valuenow="pct"
    >
      <svg viewBox="0 0 100 100" class="h-full w-full -rotate-90" aria-hidden="true">
        <circle cx="50" cy="50" :r="R" fill="none" stroke="currentColor" stroke-width="10" class="text-base-300" />
        <circle
          cx="50" cy="50" :r="R" fill="none" stroke="currentColor" stroke-width="10" stroke-linecap="round"
          class="text-primary" :stroke-dasharray="dash"
        />
      </svg>
      <span class="absolute inset-0 flex items-center justify-center text-lg font-bold" data-testid="ring-percent">{{ pct }}%</span>
    </div>
    <div>
      <p class="font-semibold"><span data-testid="progress">{{ done }}/{{ total }}</span> {{ t('today.ring.doneSuffix') }}</p>
      <p class="text-sm opacity-70">
        {{ total === 0 ? t('today.ring.none') : t('today.ring.note') }}
      </p>
    </div>
  </div>
</template>
