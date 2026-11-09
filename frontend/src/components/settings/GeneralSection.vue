<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  describeError,
  getCalendarSettings,
  putCalendarSettings,
  type CalendarSettings,
} from '../../api'
import { SUPPORTED_LOCALES, currentLocale, setLocale, useI18n } from '../../i18n'

const { t } = useI18n()
const lang = ref(currentLocale())

async function changeLanguage() {
  // Per-device preference: stored in this browser only, never sent to the server.
  lang.value = await setLocale(lang.value, { persist: true })
}

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
    list =
      (Intl as unknown as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf?.(
        'timeZone',
      ) ?? []
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
      <h2 class="card-title">{{ t('settings.general.title') }}</h2>
      <p v-if="error" class="text-error" role="alert" data-testid="general-error">{{ error }}</p>
      <label class="form-control">
        <span class="label-text">{{ t('settings.language.label') }}</span>
        <select
          v-model="lang"
          class="select select-bordered"
          data-testid="g-language"
          @change="changeLanguage"
        >
          <option v-for="l in SUPPORTED_LOCALES" :key="l.value" :value="l.value">
            {{ l.label }}
          </option>
        </select>
        <span class="label-text-alt opacity-70">{{ t('settings.language.hint') }}</span>
      </label>
      <label class="form-control">
        <span class="label-text">{{ t('settings.general.timezone') }}</span>
        <select v-model="tz" class="select select-bordered" data-testid="g-timezone">
          <option v-for="z in zones" :key="z" :value="z">{{ z }}</option>
        </select>
      </label>
      <label class="form-control">
        <span class="label-text">{{ t('settings.general.weekStart') }}</span>
        <select
          v-model.number="weekStart"
          class="select select-bordered"
          data-testid="g-week-start"
        >
          <option :value="1">{{ t('settings.general.monday') }}</option>
          <option :value="0">{{ t('settings.general.sunday') }}</option>
        </select>
      </label>
      <label class="form-control">
        <span class="label-text">{{ t('settings.general.region') }}</span>
        <select v-model="region" class="select select-bordered" data-testid="g-region">
          <!-- The server cannot clear a region once set. -->
          <option value="" :disabled="!!settings?.holiday_region">
            {{ t('settings.general.regionNone') }}
          </option>
          <option value="CN">{{ t('settings.general.regionCN') }}</option>
          <option value="US">{{ t('settings.general.regionUS') }}</option>
        </select>
      </label>
      <label class="label cursor-pointer justify-start gap-3">
        <input v-model="freeze" type="checkbox" class="checkbox" data-testid="g-freeze" />
        <span class="label-text">{{ t('settings.general.freeze') }}</span>
      </label>
      <div class="flex items-center gap-3">
        <button
          class="btn btn-primary"
          type="submit"
          :disabled="busy || !settings"
          data-testid="g-save"
        >
          {{ t('settings.general.save') }}
        </button>
        <span v-if="saved" class="text-success" data-testid="g-saved">{{
          t('settings.general.saved')
        }}</span>
      </div>
    </form>
  </section>
</template>
