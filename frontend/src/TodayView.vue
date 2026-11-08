<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { createItem, getToday, setCheckin, type TodayView } from './api'
import { usePrivacyStore } from './stores/privacy'
import { buildRRule, WEEKDAYS, type Weekday } from './rrule'

const WEEKDAY_NAMES = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']

const privacy = usePrivacyStore()

const view = ref<TodayView | null>(null)
const error = ref('')

const dueItems = computed(() => (view.value?.items ?? []).filter((i) => i.due && !i.done))
const doneItems = computed(() => (view.value?.items ?? []).filter((i) => i.done))

// Only the latest refresh may update state; stale responses are dropped.
let refreshSeq = 0

async function refresh() {
  const seq = ++refreshSeq
  try {
    const v = await getToday(privacy.redact)
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
  () => privacy.redact,
  (on) => {
    // Drop any plaintext immediately; it stays cleared if the refetch fails.
    if (on) view.value = null
    return refresh()
  },
)
</script>

<template>
  <section class="mx-auto max-w-2xl p-4">
    <header v-if="view" class="flex items-baseline justify-between">
      <h1 class="text-2xl font-bold">Today {{ view.date }} <small>{{ WEEKDAY_NAMES[view.weekday] ?? '' }}</small></h1>
      <span data-testid="progress">{{ view.progress.done }}/{{ view.progress.total }}</span>
    </header>

    <p v-if="error" class="error my-2" role="alert" data-testid="error">{{ error }}</p>

    <h2 class="mt-4 text-lg font-semibold">To do ({{ dueItems.length }})</h2>
    <ul data-testid="due-list" class="space-y-2">
      <li v-for="item in dueItems" :key="item.id" class="flex items-center justify-between rounded-box border border-base-300 bg-base-100 p-2">
        <span class="name" :class="{ sensitive: item.is_sensitive }">{{ item.name }}</span>
        <button type="button" class="btn btn-sm" :disabled="busy.includes(item.id)" @click="toggle(item.id, true)">Check</button>
      </li>
    </ul>

    <h2 class="mt-4 text-lg font-semibold">Done ({{ doneItems.length }})</h2>
    <ul data-testid="done-list" class="space-y-2">
      <li v-for="item in doneItems" :key="item.id" class="done flex items-center justify-between rounded-box border border-base-300 bg-base-100 p-2">
        <span class="name" :class="{ sensitive: item.is_sensitive }">{{ item.name }}</span>
        <button type="button" class="btn btn-sm" :disabled="busy.includes(item.id)" @click="toggle(item.id, false)">Undo</button>
      </li>
    </ul>

    <h2 class="mt-4 text-lg font-semibold">New item</h2>
    <form class="flex flex-wrap items-center gap-2" @submit.prevent="submit">
      <input v-model="name" class="input input-sm input-bordered" aria-label="Name" placeholder="Name" />
      <select v-model="presetKind" class="select select-sm select-bordered" aria-label="Repeat">
        <option value="days">Every N days</option>
        <option value="weekly">Weekly on</option>
      </select>
      <input
        v-if="presetKind === 'days'"
        v-model.number="interval"
        type="number"
        class="input input-sm input-bordered w-20"
        min="1"
        aria-label="Interval in days"
      />
      <fieldset v-else>
        <label v-for="d in WEEKDAYS" :key="d">
          <input v-model="days" type="checkbox" :value="d" /> {{ d }}
        </label>
      </fieldset>
      <button type="submit" class="btn btn-sm btn-primary">Create</button>
    </form>
  </section>
</template>
