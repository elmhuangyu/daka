<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { useI18n } from '../i18n'
import {
  buildGrid,
  cellLabel,
  levelBucket,
  monthLabels,
  weekdayNames,
  type HeatmapCell,
} from '../items/heatmap'

const props = withDefaults(
  defineProps<{
    days: HeatmapCell[]
    /** 0 = Sunday, 1 = Monday. */
    weekStart?: number
    /** Negative-polarity item: any occurrence is shown in the error colour. */
    negative?: boolean
    /** Sensitive redaction: tooltips carry no counts or names. */
    redact?: boolean
    label?: string
  }>(),
  { weekStart: 1, negative: false, redact: false, label: '' },
)
const { t } = useI18n()
const gridLabel = computed(() => props.label || t('heatmap.label'))

const root = ref<HTMLElement | null>(null)
const grid = computed(() => buildGrid(props.days, props.weekStart))
const months = computed(() => monthLabels(grid.value))
const weekdays = computed(() => weekdayNames(props.weekStart))
const lastDate = computed(() => props.days.reduce((m, d) => (d.date > m ? d.date : m), ''))

const hovered = ref('')
const focusDate = ref('')
// Roving tabindex: exactly one cell is in the tab order (the focused one, else the latest day).
const tabDate = computed(() => focusDate.value || lastDate.value)

const palette = computed(() =>
  props.negative
    ? ['bg-base-content/10', 'bg-error', 'bg-error', 'bg-error', 'bg-error']
    : ['bg-base-content/10', 'bg-success/30', 'bg-success/55', 'bg-success/80', 'bg-success'],
)
const labelOf = (c: HeatmapCell) => cellLabel(c, { negative: props.negative, redact: props.redact })
const tip = computed(() => {
  const d = hovered.value || focusDate.value
  const c = d ? props.days.find((x) => x.date === d) : undefined
  return c ? labelOf(c) : ''
})
const legend = computed(() =>
  props.negative
    ? [
        { cls: palette.value[0], text: t('heatmap.legend.none') },
        { cls: palette.value[4], text: t('heatmap.legend.occurred'), mark: true },
      ]
    : [
        { cls: palette.value[0], text: t('heatmap.legend.none') },
        { cls: palette.value[1], text: t('heatmap.legend.some') },
        { cls: palette.value[3], text: t('heatmap.legend.most') },
        { cls: palette.value[4], text: t('heatmap.legend.all'), mark: true },
      ],
)

function shift(date: string, days: number): string {
  return new Date(Date.parse(`${date}T00:00:00Z`) + days * 86_400_000).toISOString().slice(0, 10)
}

async function onKey(e: KeyboardEvent, date: string) {
  const step: Record<string, number> = { ArrowLeft: -7, ArrowRight: 7, ArrowUp: -1, ArrowDown: 1 }
  const d = step[e.key]
  if (d === undefined) return
  e.preventDefault()
  const target = shift(date, d)
  if (!props.days.some((x) => x.date === target)) return
  focusDate.value = target
  await nextTick()
  root.value?.querySelector<HTMLElement>(`[data-date="${target}"]`)?.focus()
}
</script>

<template>
  <div ref="root" data-testid="heatmap" class="inline-block max-w-full">
    <div class="flex gap-1">
      <div
        class="mt-4 flex shrink-0 flex-col gap-0.5 pr-1 text-[10px] leading-3 opacity-70"
        aria-hidden="true"
      >
        <span v-for="(w, i) in weekdays" :key="w" class="h-3">{{ i % 2 === 1 ? w : '' }}</span>
      </div>
      <div class="overflow-x-auto pb-1">
        <div
          class="flex gap-0.5 text-[10px] leading-3 opacity-70"
          aria-hidden="true"
          data-testid="heatmap-months"
        >
          <span v-for="(m, ci) in months" :key="ci" class="relative h-4 w-3 shrink-0">
            <span class="absolute left-0 whitespace-nowrap">{{ m }}</span>
          </span>
        </div>
        <div class="flex gap-0.5" role="grid" :aria-label="gridLabel">
          <div v-for="(col, ci) in grid" :key="ci" role="row" class="flex flex-col gap-0.5">
            <template v-for="(cell, ri) in col" :key="ri">
              <button
                v-if="cell"
                type="button"
                role="gridcell"
                class="relative block size-3 rounded-sm outline-offset-1 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary"
                :class="[
                  palette[levelBucket(cell.level)],
                  levelBucket(cell.level) === 4 ? 'ring-1 ring-inset ring-base-content/60' : '',
                ]"
                :data-date="cell.date"
                :data-level="levelBucket(cell.level)"
                :tabindex="cell.date === tabDate ? 0 : -1"
                :aria-label="labelOf(cell)"
                :title="labelOf(cell)"
                @mouseenter="hovered = cell.date"
                @mouseleave="hovered = ''"
                @focus="focusDate = cell.date"
                @blur="focusDate = ''"
                @keydown="onKey($event, cell.date)"
              ></button>
              <span v-else class="block size-3" aria-hidden="true"></span>
            </template>
          </div>
        </div>
      </div>
    </div>
    <p class="mt-1 min-h-4 text-xs" aria-live="polite" data-testid="heatmap-tip">{{ tip }}</p>
    <ul
      class="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs opacity-80"
      data-testid="heatmap-legend"
    >
      <li v-for="l in legend" :key="l.text" class="flex items-center gap-1">
        <span
          class="inline-block size-3 rounded-sm"
          :class="[l.cls, l.mark ? 'ring-1 ring-inset ring-base-content/60' : '']"
        ></span
        >{{ l.text }}
      </li>
    </ul>
  </div>
</template>
