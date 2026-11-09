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
  <div class="card card-border bg-base-100 border-base-300 p-4 sm:p-5 shadow-xs" data-testid="ring">
    <div class="flex items-center gap-5">
      <div
        class="relative h-20 w-20 sm:h-22 sm:w-22 shrink-0 drop-shadow-xs"
        role="progressbar"
        :aria-label="t('today.ring.label')"
        aria-valuemin="0"
        aria-valuemax="100"
        :aria-valuenow="pct"
      >
        <svg viewBox="0 0 100 100" class="h-full w-full -rotate-90" aria-hidden="true">
          <circle
            cx="50"
            cy="50"
            :r="R"
            fill="none"
            stroke="currentColor"
            stroke-width="9"
            class="text-base-300/70"
          />
          <circle
            cx="50"
            cy="50"
            :r="R"
            fill="none"
            stroke="currentColor"
            stroke-width="9"
            stroke-linecap="round"
            class="text-primary transition-all duration-500 ease-out"
            :stroke-dasharray="dash"
          />
        </svg>
        <span
          class="absolute inset-0 flex items-center justify-center text-lg sm:text-xl font-bold tracking-tight text-base-content"
          data-testid="ring-percent"
          >{{ pct }}%</span
        >
      </div>
      <div class="flex-1 min-w-0">
        <div class="flex items-center gap-2 mb-0.5">
          <p class="text-base sm:text-lg font-bold text-base-content">
            <span data-testid="progress">{{ done }}/{{ total }}</span>
            {{ t('today.ring.doneSuffix') }}
          </p>
          <span v-if="total > 0 && done >= total" class="badge badge-success badge-sm font-medium">
            {{ t('today.section.done') }}
          </span>
        </div>
        <p class="text-xs sm:text-sm text-base-content/70">
          {{ total === 0 ? t('today.ring.none') : t('today.ring.note') }}
        </p>
      </div>
    </div>
  </div>
</template>
