<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Icon } from '@iconify/vue'
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

function getEffectiveTheme(): string {
  try {
    const attr =
      typeof document !== 'undefined' ? document.documentElement.getAttribute('data-theme') : null
    if (attr) return attr
    const saved = typeof localStorage !== 'undefined' ? localStorage.getItem('theme') : null
    if (saved) return saved
  } catch {
    // storage may be restricted
  }
  if (
    typeof window !== 'undefined' &&
    window.matchMedia?.('(prefers-color-scheme: dark)').matches
  ) {
    return 'dark'
  }
  return 'light'
}

const theme = ref(getEffectiveTheme())

function changeTheme() {
  document.documentElement.setAttribute('data-theme', theme.value)
  try {
    localStorage.setItem('theme', theme.value)
  } catch {
    // localStorage may be unavailable
  }
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
  <section
    class="card bg-base-200/50 border border-base-300 shadow-sm"
    data-testid="section-general"
  >
    <form class="card-body p-6 gap-6" @submit.prevent="save">
      <div class="flex items-center gap-3 border-b border-base-300/80 pb-4">
        <div class="flex size-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
          <Icon icon="lucide:sliders" class="size-5" aria-hidden="true" />
        </div>
        <div>
          <h2 class="text-lg font-bold leading-none">{{ t('settings.general.title') }}</h2>
        </div>
      </div>

      <div
        v-if="error"
        class="alert alert-error text-sm py-2.5 px-4 shadow-sm"
        role="alert"
        data-testid="general-error"
      >
        <span>{{ error }}</span>
      </div>

      <div class="grid grid-cols-1 md:grid-cols-2 gap-5">
        <label class="form-control gap-1.5">
          <span class="label-text font-medium text-xs">{{ t('settings.language.label') }}</span>
          <select
            v-model="lang"
            class="select select-bordered select-sm md:select-md w-full"
            data-testid="g-language"
            @change="changeLanguage"
          >
            <option v-for="l in SUPPORTED_LOCALES" :key="l.value" :value="l.value">
              {{ l.label }}
            </option>
          </select>
          <span class="text-[11px] opacity-60">{{ t('settings.language.hint') }}</span>
        </label>

        <label class="form-control gap-1.5">
          <span class="label-text font-medium text-xs">{{ t('settings.theme.label') }}</span>
          <select
            v-model="theme"
            class="select select-bordered select-sm md:select-md w-full"
            data-testid="g-theme"
            @change="changeTheme"
          >
            <optgroup :label="t('settings.theme.standard')">
              <option value="light">{{ t('settings.theme.light') }}</option>
              <option value="dark">{{ t('settings.theme.dark') }}</option>
            </optgroup>
            <optgroup :label="t('settings.theme.catppuccin')">
              <option value="latte">{{ t('settings.theme.latte') }}</option>
              <option value="frappe">{{ t('settings.theme.frappe') }}</option>
              <option value="macchiato">{{ t('settings.theme.macchiato') }}</option>
              <option value="mocha">{{ t('settings.theme.mocha') }}</option>
            </optgroup>
          </select>
          <span class="text-[11px] opacity-60">{{ t('settings.theme.hint') }}</span>
        </label>

        <label class="form-control gap-1.5">
          <span class="label-text font-medium text-xs">{{ t('settings.general.timezone') }}</span>
          <select
            v-model="tz"
            class="select select-bordered select-sm md:select-md w-full"
            data-testid="g-timezone"
          >
            <option v-for="z in zones" :key="z" :value="z">{{ z }}</option>
          </select>
        </label>

        <label class="form-control gap-1.5">
          <span class="label-text font-medium text-xs">{{ t('settings.general.weekStart') }}</span>
          <select
            v-model.number="weekStart"
            class="select select-bordered select-sm md:select-md w-full"
            data-testid="g-week-start"
          >
            <option :value="1">{{ t('settings.general.monday') }}</option>
            <option :value="0">{{ t('settings.general.sunday') }}</option>
          </select>
        </label>

        <label class="form-control gap-1.5">
          <span class="label-text font-medium text-xs">{{ t('settings.general.region') }}</span>
          <select
            v-model="region"
            class="select select-bordered select-sm md:select-md w-full"
            data-testid="g-region"
          >
            <!-- The server cannot clear a region once set. -->
            <option value="" :disabled="!!settings?.holiday_region">
              {{ t('settings.general.regionNone') }}
            </option>
            <option value="CN">{{ t('settings.general.regionCN') }}</option>
            <option value="US">{{ t('settings.general.regionUS') }}</option>
          </select>
        </label>
      </div>

      <label
        class="rounded-xl border border-base-300 bg-base-100/50 p-4 flex items-center justify-between gap-4 cursor-pointer hover:bg-base-100/80 transition-colors"
      >
        <div class="flex flex-col gap-0.5">
          <span class="text-sm font-medium">{{ t('settings.general.freeze') }}</span>
        </div>
        <input
          v-model="freeze"
          type="checkbox"
          class="checkbox checkbox-primary checkbox-sm"
          data-testid="g-freeze"
        />
      </label>

      <div class="flex items-center justify-between border-t border-base-300/80 pt-4">
        <div class="flex items-center gap-2">
          <span
            v-if="saved"
            class="badge badge-success gap-1.5 py-3 px-3 text-xs"
            data-testid="g-saved"
          >
            <Icon icon="lucide:check" class="size-3.5" aria-hidden="true" />
            {{ t('settings.general.saved') }}
          </span>
        </div>
        <button
          class="btn btn-primary btn-sm md:btn-md min-w-24 gap-1.5"
          type="submit"
          :disabled="busy || !settings"
          data-testid="g-save"
        >
          <span v-if="busy" class="loading loading-spinner loading-xs" aria-hidden="true"></span>
          {{ t('settings.general.save') }}
        </button>
      </div>
    </form>
  </section>
</template>
