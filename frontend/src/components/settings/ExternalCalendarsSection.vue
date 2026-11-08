<script setup lang="ts">
import { onMounted, ref } from 'vue'
import {
  describeError, getCalendarSettings, getExternalStatus, putCalendarSettings,
  type CalendarSettings, type ExternalCalendarDto, type ExternalSourceStatus,
} from '../../api'
import { useI18n } from '../../i18n'
import { extCalErrorKey } from '../../i18n/errors'

const { t } = useI18n()

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
      <h2 class="card-title">{{ t('settings.external.title') }}</h2>
      <p class="text-sm opacity-80">{{ t('settings.external.help') }}</p>
      <p v-if="error" class="text-error" role="alert" data-testid="ext-error">{{ error }}</p>
      <ul class="flex flex-col gap-2">
        <li v-for="c in sources" :key="c.id" class="flex flex-wrap items-center gap-2" data-testid="ext-row">
          <span class="font-medium">{{ c.name }}</span>
          <code class="text-xs opacity-70" data-testid="ext-hint">{{ c.url_hint }}</code>
          <span v-if="status[c.id]" class="badge" :class="status[c.id].ok ? 'badge-success' : 'badge-error'" data-testid="ext-status">
            {{ status[c.id].ok ? t('settings.external.ok') : status[c.id].error ? t(extCalErrorKey(status[c.id].error!)) : t('settings.external.failed') }}
          </span>
          <span class="flex-1" />
          <button class="btn btn-sm btn-ghost text-error" data-testid="ext-remove" @click="remove(c)">{{ t('settings.external.remove') }}</button>
        </li>
        <li v-if="!sources.length" class="opacity-70">{{ t('settings.external.empty') }}</li>
      </ul>
      <button v-if="sources.length" class="btn btn-sm btn-outline self-start" data-testid="ext-check" @click="check">{{ t('settings.external.check') }}</button>
      <form class="flex flex-wrap items-end gap-2" @submit.prevent="add">
        <input v-model="newName" class="input input-bordered input-sm" :placeholder="t('settings.external.name')" maxlength="64" data-testid="ext-name" />
        <input v-model="newUrl" class="input input-bordered input-sm min-w-60 flex-1" :placeholder="t('settings.external.urlPlaceholder')" type="url" data-testid="ext-url" />
        <button class="btn btn-sm btn-primary" type="submit" data-testid="ext-add">{{ t('settings.external.add') }}</button>
      </form>
    </div>
  </section>
</template>
