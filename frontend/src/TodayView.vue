<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { createItem, getToday, setCheckin, type TodayView } from './api'
import { buildRRule, WEEKDAYS, type Weekday } from './rrule'

const WEEKDAY_NAMES = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']

const props = defineProps<{ redact: boolean }>()

const view = ref<TodayView | null>(null)
const error = ref('')

const dueItems = computed(() => (view.value?.items ?? []).filter((i) => i.due && !i.done))
const doneItems = computed(() => (view.value?.items ?? []).filter((i) => i.done))

// Only the latest refresh may update state; stale responses are dropped.
let refreshSeq = 0

async function refresh() {
  const seq = ++refreshSeq
  try {
    const v = await getToday(props.redact)
    if (seq !== refreshSeq) return
    view.value = v
    error.value = ''
  } catch (e) {
    if (seq !== refreshSeq) return
    error.value = e instanceof Error ? e.message : String(e)
  }
}

// Item ids with a check/undo request in flight.
const busy = ref<number[]>([])

async function toggle(id: number, done: boolean) {
  if (busy.value.includes(id)) return
  busy.value.push(id)
  try {
    await setCheckin(id, done)
    error.value = ''
    await refresh()
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = busy.value.filter((x) => x !== id)
  }
}

// Create form
const name = ref('')
const presetKind = ref<'days' | 'weekly'>('days')
const interval = ref(1)
const days = ref<Weekday[]>([])

async function submit() {
  let rrule: string
  try {
    rrule =
      presetKind.value === 'days'
        ? buildRRule({ kind: 'days', interval: interval.value })
        : buildRRule({ kind: 'weekly', days: days.value })
  } catch (e) {
    error.value = (e as Error).message
    return
  }
  if (!name.value.trim()) {
    error.value = 'Name is required'
    return
  }
  try {
    await createItem({ name: name.value.trim(), rrule_string: rrule })
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
    return
  }
  name.value = ''
  await refresh()
}

onMounted(refresh)
watch(
  () => props.redact,
  (on) => {
    // Drop any plaintext immediately; it stays cleared if the refetch fails.
    if (on) view.value = null
    return refresh()
  },
)
</script>

<template>
  <section>
    <header v-if="view">
      <h1>Today {{ view.date }} <small>{{ WEEKDAY_NAMES[view.weekday] ?? '' }}</small></h1>
      <span data-testid="progress">{{ view.progress.done }}/{{ view.progress.total }}</span>
    </header>

    <p v-if="error" class="error" role="alert" data-testid="error">{{ error }}</p>

    <h2>To do ({{ dueItems.length }})</h2>
    <ul data-testid="due-list">
      <li v-for="item in dueItems" :key="item.id">
        <span class="name">{{ item.name }}</span>
        <button type="button" :disabled="busy.includes(item.id)" @click="toggle(item.id, true)">Check</button>
      </li>
    </ul>

    <h2>Done ({{ doneItems.length }})</h2>
    <ul data-testid="done-list">
      <li v-for="item in doneItems" :key="item.id" class="done">
        <span class="name">{{ item.name }}</span>
        <button type="button" :disabled="busy.includes(item.id)" @click="toggle(item.id, false)">Undo</button>
      </li>
    </ul>

    <h2>New item</h2>
    <form @submit.prevent="submit">
      <input v-model="name" aria-label="Name" placeholder="Name" />
      <select v-model="presetKind" aria-label="Repeat">
        <option value="days">Every N days</option>
        <option value="weekly">Weekly on</option>
      </select>
      <input
        v-if="presetKind === 'days'"
        v-model.number="interval"
        type="number"
        min="1"
        aria-label="Interval in days"
      />
      <fieldset v-else>
        <label v-for="d in WEEKDAYS" :key="d">
          <input v-model="days" type="checkbox" :value="d" /> {{ d }}
        </label>
      </fieldset>
      <button type="submit">Create</button>
    </form>
  </section>
</template>
