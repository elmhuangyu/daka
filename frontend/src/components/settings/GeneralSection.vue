<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { describeError, getCalendarSettings, putCalendarSettings, type CalendarSettings } from '../../api'

const settings = ref<CalendarSettings | null>(null)
const error = ref('')
const saved = ref(false)
const busy = ref(false)

const tz = ref('UTC')
const weekStart = ref(1)
const region = ref('')
const freeze = ref(false)

const zones = computed(() => {
  let list: string[] = []
  try {
    list = (Intl as unknown as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf?.('timeZone') ?? []
  } catch {
    list = []
  }
  const set = new Set(list)
  set.add('UTC')
  set.add(tz.value)
  return [...set].sort()
})

function apply(s: CalendarSettings) {
  settings.value = s
  tz.value = s.timezone
  weekStart.value = s.week_start
  region.value = s.holiday_region
  freeze.value = s.streak_holiday_freeze
}

onMounted(async () => {
  try {
    apply(await getCalendarSettings())
  } catch (e) {
    error.value = describeError(e)
  }
})

async function save() {
  busy.value = true
  error.value = ''
  saved.value = false
  try {
    apply(
      await putCalendarSettings({
        timezone: tz.value,
        week_start: weekStart.value,
        holiday_region: region.value,
        streak_holiday_freeze: freeze.value,
      }),
    )
    saved.value = true
  } catch (e) {
    error.value = describeError(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section class="card bg-base-200" data-testid="section-general">
    <form class="card-body gap-3" @submit.prevent="save">
      <h2 class="card-title">General</h2>
      <p v-if="error" class="text-error" role="alert" data-testid="general-error">{{ error }}</p>
      <label class="form-control">
        <span class="label-text">Time zone</span>
        <select v-model="tz" class="select select-bordered" data-testid="g-timezone">
          <option v-for="z in zones" :key="z" :value="z">{{ z }}</option>
        </select>
      </label>
      <label class="form-control">
        <span class="label-text">Week starts on</span>
        <select v-model.number="weekStart" class="select select-bordered" data-testid="g-week-start">
          <option :value="1">Monday</option>
          <option :value="0">Sunday</option>
        </select>
      </label>
      <label class="form-control">
        <span class="label-text">Holiday region</span>
        <select v-model="region" class="select select-bordered" data-testid="g-region">
          <!-- The server cannot clear a region once set. -->
          <option value="" :disabled="!!settings?.holiday_region">None</option>
          <option value="CN">China (CN)</option>
          <option value="US">United States (US)</option>
        </select>
      </label>
      <label class="label cursor-pointer justify-start gap-3">
        <input v-model="freeze" type="checkbox" class="checkbox" data-testid="g-freeze" />
        <span class="label-text">Holidays do not break streaks</span>
      </label>
      <div class="flex items-center gap-3">
        <button class="btn btn-primary" type="submit" :disabled="busy || !settings" data-testid="g-save">Save</button>
        <span v-if="saved" class="text-success" data-testid="g-saved">Saved</span>
      </div>
    </form>
  </section>
</template>
