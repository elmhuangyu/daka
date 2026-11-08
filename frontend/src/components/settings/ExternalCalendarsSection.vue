<script setup lang="ts">
import { onMounted, ref } from 'vue'
import {
  describeError, getCalendarSettings, getExternalStatus, putCalendarSettings,
  type CalendarSettings, type ExternalCalendarDto, type ExternalSourceStatus,
} from '../../api'

const sources = ref<ExternalCalendarDto[]>([])
const status = ref<Record<string, ExternalSourceStatus>>({})
const error = ref('')
const newName = ref('')
const newUrl = ref('')

function apply(s: CalendarSettings) {
  sources.value = s.external_calendars ?? []
}

onMounted(async () => {
  try {
    apply(await getCalendarSettings())
  } catch (e) {
    error.value = describeError(e)
  }
})

// Existing sources are sent without `url`, which keeps the stored URL.
const keep = (list: ExternalCalendarDto[]) => list.map((c) => ({ id: c.id, name: c.name }))

async function save(list: { id?: string; name: string; url?: string }[]) {
  error.value = ''
  try {
    apply(await putCalendarSettings({ external_calendars: list }))
    return true
  } catch (e) {
    error.value = describeError(e)
    return false
  }
}

async function add() {
  if (!newName.value.trim() || !newUrl.value.trim()) return
  if (await save([...keep(sources.value), { name: newName.value.trim(), url: newUrl.value.trim() }])) {
    newName.value = ''
    newUrl.value = ''
  }
}

const remove = (c: ExternalCalendarDto) => save(keep(sources.value.filter((x) => x.id !== c.id)))

async function check() {
  error.value = ''
  try {
    const d = new Date()
    const day = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
    status.value = Object.fromEntries((await getExternalStatus(day)).map((s) => [s.id, s]))
  } catch (e) {
    error.value = describeError(e)
  }
}
</script>

<template>
  <section class="card bg-base-200" data-testid="section-external">
    <div class="card-body gap-3">
      <h2 class="card-title">External calendars</h2>
      <p class="text-sm opacity-80">
        Subscribe to read-only ICS URLs to see their events next to your items. The full URL is stored encrypted and
        never shown again; only a short hint is displayed.
      </p>
      <p v-if="error" class="text-error" role="alert" data-testid="ext-error">{{ error }}</p>
      <ul class="flex flex-col gap-2">
        <li v-for="c in sources" :key="c.id" class="flex flex-wrap items-center gap-2" data-testid="ext-row">
          <span class="font-medium">{{ c.name }}</span>
          <code class="text-xs opacity-70" data-testid="ext-hint">{{ c.url_hint }}</code>
          <span v-if="status[c.id]" class="badge" :class="status[c.id].ok ? 'badge-success' : 'badge-error'" data-testid="ext-status">
            {{ status[c.id].ok ? 'OK' : status[c.id].error || 'Failed' }}
          </span>
          <span class="flex-1" />
          <button class="btn btn-sm btn-ghost text-error" data-testid="ext-remove" @click="remove(c)">Remove</button>
        </li>
        <li v-if="!sources.length" class="opacity-70">No external calendars.</li>
      </ul>
      <button v-if="sources.length" class="btn btn-sm btn-outline self-start" data-testid="ext-check" @click="check">Check status</button>
      <form class="flex flex-wrap items-end gap-2" @submit.prevent="add">
        <input v-model="newName" class="input input-bordered input-sm" placeholder="Name" maxlength="64" data-testid="ext-name" />
        <input v-model="newUrl" class="input input-bordered input-sm min-w-60 flex-1" placeholder="https://.../calendar.ics" type="url" data-testid="ext-url" />
        <button class="btn btn-sm btn-primary" type="submit" data-testid="ext-add">Add</button>
      </form>
    </div>
  </section>
</template>
