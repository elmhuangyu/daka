<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Icon } from '@iconify/vue'
import type { CalendarDayItem, CheckinInput } from '../../api'
import { DAY_STATUS_TEXT, canMakeUp, eventTime, longDate } from '../../calendar/logic'
import { useCalendarStore } from '../../stores/calendar'
import MakeupControl from './MakeupControl.vue'

const store = useCalendarStore()
const emit = defineEmits<{ close: [] }>()

const day = computed(() => store.selectedDay)
const date = computed(() => store.selected ?? '')
const today = computed(() => store.view?.today ?? '')
const editable = computed(() => canMakeUp(date.value, today.value))
const external = computed(() => store.externalByDate.get(date.value) ?? [])

// Items added by hand for a retroactive entry although they were not scheduled.
const added = ref<number[]>([])
const pick = ref('')
watch(date, () => {
  added.value = []
  pick.value = ''
})

const listed = computed(() => (day.value?.items ?? []).filter((i) => !i.forecast || i.overdue))
const forecast = computed(() => (day.value?.items ?? []).filter((i) => i.forecast && !i.overdue))
const shownIds = computed(() => new Set([...listed.value.map((i) => i.item_id), ...added.value]))
const addable = computed(() => store.items.filter((d) => !shownIds.value.has(d.id)))
const addedDetails = computed(() => added.value.flatMap((id) => store.detailById.get(id) ?? []))

function addPicked() {
  const id = Number(pick.value)
  if (id && !added.value.includes(id)) added.value.push(id)
  pick.value = ''
}

const STATUS_BADGE: Record<string, { text: string; cls: string }> = {
  done: { text: 'Done', cls: 'badge-success' },
  partial: { text: 'Partial', cls: 'badge-warning' },
  missed: { text: 'Missed', cls: 'badge-neutral' },
  frozen: { text: 'Frozen', cls: 'badge-info' },
  pending: { text: 'Open', cls: 'badge-primary' },
  upcoming: { text: 'Forecast', cls: 'badge-outline' },
  extra: { text: 'Extra', cls: 'badge-ghost' },
}
function badge(i: CalendarDayItem) {
  return i.overdue ? { text: 'Overdue', cls: 'badge-error' } : (STATUS_BADGE[i.status] ?? STATUS_BADGE.extra!)
}
const logsOf = (id: number) => store.dayLogs.filter((l) => l.item_id === id)
const makeUp = (input: Omit<CheckinInput, 'date'>) => store.makeUp(input)
</script>

<template>
  <aside
    v-if="store.selected" class="space-y-3 rounded-box border border-base-300 bg-base-100 p-3" aria-labelledby="day-panel-title"
    data-testid="day-panel"
  >
    <header class="flex items-start justify-between gap-2">
      <div>
        <h2 id="day-panel-title" class="text-lg font-semibold" tabindex="-1">{{ longDate(date) }}</h2>
        <p class="flex flex-wrap items-center gap-1 text-sm opacity-80">
          <span v-if="day?.day_type === 'holiday'" class="badge badge-success badge-sm">
            Off{{ day.holiday_name ? `: ${day.holiday_name}` : '' }}
          </span>
          <span v-else-if="day?.day_type === 'workday'" class="badge badge-error badge-sm">
            Makeup workday{{ day.holiday_name ? `: ${day.holiday_name}` : '' }}
          </span>
          <span v-if="day">{{ DAY_STATUS_TEXT[day.status] }}</span>
          <span v-if="day?.frozen" class="badge badge-info badge-sm">Frozen</span>
        </p>
      </div>
      <button type="button" class="btn btn-ghost btn-sm btn-square" aria-label="Close day panel" data-testid="panel-close" @click="emit('close')">
        <Icon icon="lucide:x" aria-hidden="true" />
      </button>
    </header>

    <p v-if="store.panelError" class="alert alert-error text-sm" role="alert" data-testid="panel-error">{{ store.panelError }}</p>

    <section v-if="external.length" aria-labelledby="panel-ext">
      <h3 id="panel-ext" class="text-sm font-semibold">External calendar</h3>
      <ul class="text-sm" data-testid="panel-external">
        <li v-for="(x, idx) in external" :key="idx" class="flex gap-2">
          <span class="w-12 shrink-0 opacity-70">{{ eventTime(x.event) }}</span>
          <span class="min-w-0 truncate">{{ x.event.title }} <span class="opacity-60">({{ x.source }})</span></span>
        </li>
      </ul>
    </section>

    <section aria-labelledby="panel-items">
      <h3 id="panel-items" class="text-sm font-semibold">{{ editable ? 'Items' : 'Planned items' }}</h3>
      <p v-if="!listed.length && !forecast.length && !addedDetails.length" class="text-sm opacity-70" data-testid="panel-empty">
        Nothing scheduled or recorded.
      </p>
      <ul class="space-y-2">
        <li v-for="i in listed" :key="i.item_id" class="rounded-box border border-base-300 p-2" :data-testid="`panel-item-${i.item_id}`">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <span class="name font-medium" :class="{ sensitive: i.is_sensitive }">{{ i.name }}</span>
            <span class="flex items-center gap-1">
              <span v-if="i.has_supplement" class="badge badge-ghost badge-sm">Makeup</span>
              <span class="badge badge-sm" :class="badge(i).cls">{{ badge(i).text }}</span>
            </span>
          </div>
          <p v-if="i.overdue" class="text-sm text-error">Overdue: shown on today until done.</p>
          <div v-if="editable && !(i.forecast && !i.overdue)" class="mt-2">
            <MakeupControl
              v-if="store.detailById.get(i.item_id)" :detail="store.detailById.get(i.item_id)!" :logs="logsOf(i.item_id)"
              :busy="store.busy.includes(i.item_id)" :date="date" @log="makeUp" @remove="store.removeLog(i.item_id, $event)"
            />
            <p v-else class="text-sm opacity-60">Loading controls...</p>
          </div>
        </li>
        <li
          v-for="d in addedDetails" :key="`add-${d.id}`" class="rounded-box border border-dashed border-base-300 p-2"
          :data-testid="`panel-added-${d.id}`"
        >
          <span class="name font-medium" :class="{ sensitive: d.effective_sensitive }">{{ d.name }}</span>
          <div class="mt-2">
            <MakeupControl
              :detail="d" :logs="logsOf(d.id)" :busy="store.busy.includes(d.id)" :date="date" @log="makeUp"
              @remove="store.removeLog(d.id, $event)"
            />
          </div>
        </li>
      </ul>

      <div v-if="editable && addable.length" class="mt-3 flex items-center gap-2">
        <label class="sr-only" for="makeup-pick">Add a check-in for another item</label>
        <select id="makeup-pick" v-model="pick" class="select select-sm min-w-0 flex-1" data-testid="makeup-pick">
          <option value="">Add another item...</option>
          <option v-for="d in addable" :key="d.id" :value="String(d.id)">{{ d.name }}</option>
        </select>
        <button type="button" class="btn btn-sm" :disabled="!pick" data-testid="makeup-add" @click="addPicked">Add</button>
      </div>
    </section>

    <section v-if="forecast.length" aria-labelledby="panel-forecast">
      <h3 id="panel-forecast" class="text-sm font-semibold">Forecast</h3>
      <ul class="text-sm" data-testid="panel-forecast">
        <li v-for="i in forecast" :key="i.item_id" class="flex items-center justify-between gap-2">
          <span class="name" :class="{ sensitive: i.is_sensitive }">{{ i.name }}</span>
          <span class="badge badge-outline badge-sm">Expected</span>
        </li>
      </ul>
    </section>
  </aside>
</template>
