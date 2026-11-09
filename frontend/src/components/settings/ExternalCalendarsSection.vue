<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Icon } from '@iconify/vue'
import {
  describeError,
  getCalendarSettings,
  getExternalStatus,
  putCalendarSettings,
  type CalendarSettings,
  type ExternalCalendarDto,
  type ExternalSourceStatus,
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
  if (
    await save([...keep(sources.value), { name: newName.value.trim(), url: newUrl.value.trim() }])
  ) {
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
  <section
    class="card bg-base-200/50 border border-base-300 shadow-sm"
    data-testid="section-external"
  >
    <div class="card-body p-6 gap-6">
      <div class="flex items-center justify-between border-b border-base-300/80 pb-4">
        <div class="flex items-center gap-3">
          <div
            class="flex size-10 items-center justify-center rounded-lg bg-primary/10 text-primary"
          >
            <Icon icon="lucide:calendar" class="size-5" aria-hidden="true" />
          </div>
          <div>
            <h2 class="text-lg font-bold leading-none">{{ t('settings.external.title') }}</h2>
          </div>
        </div>
        <button
          v-if="sources.length"
          class="btn btn-xs sm:btn-sm btn-outline gap-1.5"
          data-testid="ext-check"
          @click="check"
        >
          <Icon icon="lucide:check" class="size-3.5" aria-hidden="true" />
          {{ t('settings.external.check') }}
        </button>
      </div>

      <div
        class="rounded-xl bg-base-100/50 border border-base-300 p-3.5 text-xs leading-relaxed opacity-80 flex items-start gap-2.5"
      >
        <Icon icon="lucide:info" class="size-4 shrink-0 text-info mt-0.5" aria-hidden="true" />
        <span>{{ t('settings.external.help') }}</span>
      </div>

      <div
        v-if="error"
        class="alert alert-error text-sm py-2.5 px-4 shadow-sm"
        role="alert"
        data-testid="ext-error"
      >
        <span>{{ error }}</span>
      </div>

      <ul class="flex flex-col gap-2">
        <li
          v-for="c in sources"
          :key="c.id"
          class="rounded-xl border border-base-300 bg-base-100/60 p-3.5 flex flex-wrap items-center justify-between gap-3 transition-colors hover:bg-base-100/90"
          data-testid="ext-row"
        >
          <div class="flex flex-col gap-1 min-w-0">
            <div class="flex items-center gap-2">
              <span class="font-semibold text-sm">{{ c.name }}</span>
              <span
                v-if="status[c.id]"
                class="badge badge-sm"
                :class="status[c.id].ok ? 'badge-success' : 'badge-error'"
                data-testid="ext-status"
              >
                {{
                  status[c.id].ok
                    ? t('settings.external.ok')
                    : status[c.id].error
                      ? t(extCalErrorKey(status[c.id].error!))
                      : t('settings.external.failed')
                }}
              </span>
            </div>
            <code class="text-xs opacity-60 font-mono" data-testid="ext-hint">{{
              c.url_hint
            }}</code>
          </div>
          <button
            class="btn btn-sm btn-ghost text-error hover:bg-error/10 text-xs"
            data-testid="ext-remove"
            @click="remove(c)"
          >
            {{ t('settings.external.remove') }}
          </button>
        </li>
        <li
          v-if="!sources.length"
          class="rounded-xl border border-dashed border-base-300 p-8 text-center flex flex-col items-center justify-center gap-2"
        >
          <Icon icon="lucide:calendar" class="size-8 opacity-25" aria-hidden="true" />
          <span class="text-sm opacity-70">{{ t('settings.external.empty') }}</span>
        </li>
      </ul>

      <form
        class="rounded-xl border border-base-300 bg-base-100/40 p-3.5 flex flex-col sm:flex-row items-stretch sm:items-end gap-2.5"
        @submit.prevent="add"
      >
        <label class="form-control gap-1 sm:w-48">
          <span class="label-text text-xs font-medium">{{ t('settings.external.name') }}</span>
          <input
            v-model="newName"
            class="input input-bordered input-sm"
            :placeholder="t('settings.external.name')"
            maxlength="64"
            data-testid="ext-name"
          />
        </label>
        <label class="form-control gap-1 flex-1">
          <input
            v-model="newUrl"
            class="input input-bordered input-sm w-full"
            :placeholder="t('settings.external.urlPlaceholder')"
            type="url"
            data-testid="ext-url"
          />
        </label>
        <button
          class="btn btn-sm btn-primary gap-1.5 sm:self-end"
          type="submit"
          data-testid="ext-add"
        >
          <Icon icon="lucide:plus" class="size-3.5" aria-hidden="true" />
          {{ t('settings.external.add') }}
        </button>
      </form>
    </div>
  </section>
</template>
