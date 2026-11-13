<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { percent } from '../../today/logic'
import { useI18n } from '../../i18n'

const { t } = useI18n()

const props = defineProps<{
  done: number
  total: number
  todoDone?: number
  todoTotal?: number
}>()
const todoDone = computed(() => props.todoDone ?? 0)
const todoTotal = computed(() => props.todoTotal ?? 0)
const allDone = computed(() => props.done + todoDone.value)
const allTotal = computed(() => props.total + todoTotal.value)
const pct = computed(() => percent(allDone.value, allTotal.value))
// Outer ring: habit check-ins. Inner ring: to-dos (a different colour so one cannot hide the other).
const R1 = 44
const R2 = 32
const C1 = 2 * Math.PI * R1
const C2 = 2 * Math.PI * R2
const dash1 = computed(() => `${(percent(props.done, props.total) / 100) * C1} ${C1}`)
const dash2 = computed(() => `${(percent(todoDone.value, todoTotal.value) / 100) * C2} ${C2}`)
// Progress tier drives the ambient animation: idle, warming up, almost there, complete.
const tier = computed(() => {
  if (allTotal.value === 0) return 'none'
  if (pct.value >= 100) return 'full'
  if (pct.value >= 75) return 'high'
  if (pct.value >= 40) return 'mid'
  return 'low'
})
// Re-trigger a short "pop" every time progress goes up.
const pop = ref(false)
watch(pct, (n, o) => {
  if (n <= o) return
  pop.value = false
  requestAnimationFrame(() => (pop.value = true))
})
</script>

<template>
  <div class="card card-border bg-base-100 border-base-300 p-4 sm:p-5 shadow-xs" data-testid="ring">
    <div class="flex items-center gap-5">
      <div
        class="ring-wrap relative h-20 w-20 sm:h-22 sm:w-22 shrink-0 drop-shadow-xs"
        :class="[`ring-${tier}`, { 'ring-pop': pop }]"
        @animationend.self="pop = false"
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
            :r="R1"
            fill="none"
            stroke="currentColor"
            stroke-width="8"
            class="text-base-300/70"
          />
          <circle
            cx="50"
            cy="50"
            :r="R1"
            fill="none"
            stroke="currentColor"
            stroke-width="8"
            stroke-linecap="round"
            v-if="done > 0"
            class="text-primary transition-all duration-500 ease-out"
            :stroke-dasharray="dash1"
          />
          <circle
            cx="50"
            cy="50"
            :r="R2"
            fill="none"
            stroke="currentColor"
            stroke-width="8"
            class="text-base-300/70"
          />
          <circle
            cx="50"
            cy="50"
            :r="R2"
            fill="none"
            stroke="currentColor"
            stroke-width="8"
            stroke-linecap="round"
            v-if="todoDone > 0"
            class="text-secondary transition-all duration-500 ease-out"
            :stroke-dasharray="dash2"
          />
        </svg>
        <span
          class="absolute inset-0 flex items-center justify-center text-base font-bold tracking-tight text-base-content"
          data-testid="ring-percent"
          >{{ pct }}%</span
        >
      </div>
      <div class="flex-1 min-w-0 space-y-1">
        <p v-if="allTotal === 0" class="text-sm text-base-content/70">{{ t('today.ring.none') }}</p>
        <template v-else>
          <p class="flex items-center gap-2 text-sm sm:text-base font-semibold">
            <span class="size-2.5 rounded-full bg-primary shrink-0" aria-hidden="true"></span>
            {{ t('today.ring.habits') }}
            <span data-testid="progress">{{ done }}/{{ total }}</span>
          </p>
          <p class="flex items-center gap-2 text-sm sm:text-base font-semibold">
            <span class="size-2.5 rounded-full bg-secondary shrink-0" aria-hidden="true"></span>
            {{ t('today.ring.todos') }}
            <span data-testid="todo-progress">{{ todoDone }}/{{ todoTotal }}</span>
          </p>
          <span v-if="allDone >= allTotal" class="badge badge-success badge-sm font-medium">
            {{ t('today.section.done') }}
          </span>
        </template>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ring-wrap {
  border-radius: 9999px;
}
.ring-pop {
  animation: ring-pop 0.5s cubic-bezier(0.3, 1.6, 0.5, 1);
}
.ring-mid {
  animation: ring-breathe 4s ease-in-out infinite;
  --glow: color-mix(in oklab, var(--color-primary) 35%, transparent);
}
.ring-high {
  animation: ring-breathe 2.6s ease-in-out infinite;
  --glow: color-mix(in oklab, var(--color-primary) 55%, transparent);
}
.ring-full {
  animation: ring-breathe 1.8s ease-in-out infinite;
  --glow: color-mix(in oklab, var(--color-success) 70%, transparent);
}
.ring-full svg {
  filter: hue-rotate(0deg);
  animation: ring-shine 3s linear infinite;
}
.ring-pop.ring-mid,
.ring-pop.ring-high,
.ring-pop.ring-full {
  animation: ring-pop 0.5s cubic-bezier(0.3, 1.6, 0.5, 1);
}
@keyframes ring-pop {
  0% {
    transform: scale(1);
  }
  40% {
    transform: scale(1.14);
  }
  100% {
    transform: scale(1);
  }
}
@keyframes ring-breathe {
  0%,
  100% {
    box-shadow: 0 0 0 0 transparent;
  }
  50% {
    box-shadow: 0 0 22px 6px var(--glow);
  }
}
@keyframes ring-shine {
  0%,
  100% {
    filter: brightness(1);
  }
  50% {
    filter: brightness(1.25) saturate(1.2);
  }
}
@media (prefers-reduced-motion: reduce) {
  .ring-wrap,
  .ring-wrap svg {
    animation: none !important;
  }
}
</style>
