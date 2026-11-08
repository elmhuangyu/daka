<script setup lang="ts">
import { computed } from 'vue'
import { Icon } from '@iconify/vue'
import type { CalendarDay, CalendarDayItem, ExternalEvent } from '../../api'
import { RIBBON_CLASS, dayLabel, dayNumber, eventTime } from '../../calendar/logic'

const props = defineProps<{
  date: string
  day: CalendarDay | null
  today: string
  inMonth: boolean
  selected: boolean
  focusable: boolean
  expanded: boolean
  external: { source: string; event: ExternalEvent }[]
}>()
const emit = defineEmits<{ select: [string] }>()

const isToday = computed(() => props.date === props.today)
const MAX_DOTS = 5
const dueItems = computed(() => (props.day?.items ?? []).filter((i) => i.due || i.log_count > 0))
const dots = computed(() => dueItems.value.slice(0, MAX_DOTS))
const extraDots = computed(() => Math.max(0, dueItems.value.length - MAX_DOTS))

function dotClass(i: CalendarDayItem): string {
  if (i.overdue) return 'bg-error ring-2 ring-error/40'
  switch (i.status) {
    case 'done': return 'bg-success'
    case 'partial': return 'bg-warning'
    case 'missed': return 'bg-base-content/40'
    case 'frozen': return 'bg-info/60'
    case 'pending': return 'bg-primary'
    case 'upcoming': return 'border border-base-content/50 bg-transparent'
    default: return 'bg-secondary/60'
  }
}
const label = computed(() => {
  const base = props.day ? dayLabel(props.day) : props.date
  return props.external.length ? `${base}, ${props.external.length} external event${props.external.length === 1 ? '' : 's'}` : base
})
</script>

<template>
  <button
    type="button"
    role="gridcell"
    class="relative flex min-w-0 flex-col gap-1 overflow-hidden border border-base-300 p-1 text-left align-top hover:bg-base-200 focus-visible:outline-2 focus-visible:outline-primary"
    :class="[
      expanded ? 'min-h-32' : 'min-h-16 sm:min-h-24',
      inMonth ? 'bg-base-100' : 'bg-base-200/60 text-base-content/60',
      selected ? 'outline-2 -outline-offset-2 outline-primary' : '',
    ]"
    :tabindex="focusable ? 0 : -1"
    :aria-selected="selected"
    :aria-current="isToday ? 'date' : undefined"
    :aria-label="label"
    :data-date="date"
    :data-testid="`day-${date}`"
    @click="emit('select', date)"
  >
    <span class="flex items-start justify-between gap-1">
      <span
        class="inline-flex size-6 items-center justify-center rounded-full text-sm"
        :class="isToday ? 'bg-primary font-bold text-primary-content' : ''"
      >{{ dayNumber(date) }}</span>
      <span class="flex items-center gap-0.5">
        <Icon
          v-if="day?.has_overdue" icon="lucide:triangle-alert" class="size-4 text-error" aria-hidden="true"
          data-testid="overdue-mark"
        />
        <span
          v-if="day?.day_type === 'holiday'" class="badge badge-success badge-xs" data-testid="badge-off"
          :title="day.holiday_name || 'Holiday'" aria-hidden="true"
        >Off</span>
        <span
          v-else-if="day?.day_type === 'workday'" class="badge badge-error badge-xs" data-testid="badge-work"
          title="Makeup workday" aria-hidden="true"
        >Work</span>
      </span>
    </span>

    <span v-if="expanded && day" class="flex min-w-0 flex-col gap-0.5" aria-hidden="true">
      <span v-for="i in dueItems" :key="i.item_id" class="flex min-w-0 items-center gap-1 text-xs">
        <span class="size-2 shrink-0 rounded-full" :class="dotClass(i)"></span>
        <span class="name truncate" :class="{ sensitive: i.is_sensitive, 'opacity-70': i.forecast }">{{ i.name }}</span>
      </span>
    </span>
    <span v-else-if="dots.length" class="flex flex-wrap items-center gap-1" aria-hidden="true" data-testid="dots">
      <span v-for="i in dots" :key="i.item_id" class="size-2 rounded-full" :class="dotClass(i)"></span>
      <span v-if="extraDots" class="text-[10px] leading-none opacity-70">+{{ extraDots }}</span>
    </span>

    <span v-if="external.length" class="flex min-w-0 flex-col gap-0.5" aria-hidden="true" data-testid="external-chips">
      <template v-if="expanded">
        <span
          v-for="(x, idx) in external.slice(0, 4)" :key="idx"
          class="ext-title truncate rounded bg-base-content/10 px-1 text-[11px] opacity-80"
        >{{ eventTime(x.event) }} {{ x.event.title }}</span>
      </template>
      <span v-else class="truncate rounded bg-base-content/10 px-1 text-[11px] opacity-80">{{ external.length }} ext.</span>
    </span>

    <span
      class="absolute inset-x-0 bottom-0 h-1.5" :class="day ? RIBBON_CLASS[day.status] : ''" aria-hidden="true"
      :data-ribbon="day?.status"
    ></span>
  </button>
</template>
