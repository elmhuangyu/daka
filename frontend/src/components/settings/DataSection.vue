<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Icon } from '@iconify/vue'
import { deleteAccount, describeError, exportData, importData, type ImportSummary } from '../../api'
import { useAuthStore } from '../../stores/auth'
import { useI18n } from '../../i18n'

const MAX_IMPORT_BYTES = 64 << 20
const CONFIRM_WORD = 'DELETE'

const { t } = useI18n()
const auth = useAuthStore()
const router = useRouter()

const error = ref('')
const notice = ref('')
const busy = ref(false)

// The file text lives only in this ref until imported or replaced.
const fileText = ref('')
const summary = ref<ImportSummary | null>(null)
const applySettings = ref(false)
const confirmText = ref('')
const canDelete = computed(() => confirmText.value === CONFIRM_WORD && !busy.value)

function reset() {
  error.value = ''
  notice.value = ''
}

async function doExport() {
  reset()
  busy.value = true
  try {
    const text = await exportData()
    const url = URL.createObjectURL(new Blob([text], { type: 'application/json' }))
    try {
      const a = document.createElement('a')
      a.href = url
      a.download = `daka-export-${new Date().toISOString().slice(0, 10)}.json`
      a.click()
    } finally {
      URL.revokeObjectURL(url)
    }
    notice.value = t('data.export.done')
  } catch (e) {
    error.value = describeError(e)
  } finally {
    busy.value = false
  }
}

async function onFile(ev: Event) {
  reset()
  fileText.value = ''
  summary.value = null
  const file = (ev.target as HTMLInputElement).files?.[0]
  if (!file) return
  if (file.size > MAX_IMPORT_BYTES) {
    error.value = t('data.import.tooLarge', { mb: MAX_IMPORT_BYTES >> 20 })
    return
  }
  busy.value = true
  try {
    try {
      fileText.value = await file.text()
    } catch {
      error.value = t('data.import.readFailed')
      return
    }
    try {
      summary.value = await importData(fileText.value, { dryRun: true })
    } catch (e) {
      fileText.value = ''
      error.value = describeError(e)
    }
  } finally {
    busy.value = false
  }
}

async function doImport() {
  if (!fileText.value) return
  reset()
  busy.value = true
  try {
    const res = await importData(fileText.value, { applySettings: applySettings.value })
    notice.value = t('data.import.done', { items: res.items, checkins: res.checkins })
    fileText.value = ''
    summary.value = null
  } catch (e) {
    error.value = describeError(e)
  } finally {
    busy.value = false
  }
}

async function doDelete() {
  if (!canDelete.value) return
  reset()
  busy.value = true
  try {
    await deleteAccount()
    confirmText.value = ''
    auth.markUnauthenticated()
    await router.push({ name: auth.loginRequired ? 'login' : 'today' })
  } catch (e) {
    error.value = describeError(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section class="card bg-base-200/50 border border-base-300 shadow-sm" data-testid="section-data">
    <div class="card-body p-6 gap-6">
      <div class="flex items-center gap-3 border-b border-base-300/80 pb-4">
        <div class="flex size-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
          <Icon icon="lucide:database" class="size-5" aria-hidden="true" />
        </div>
        <div>
          <h2 class="text-lg font-bold leading-none">{{ t('data.title') }}</h2>
          <p class="mt-1 text-xs opacity-70">{{ t('data.intro') }}</p>
        </div>
      </div>

      <div
        v-if="error"
        class="alert alert-error text-sm py-2.5 px-4 shadow-sm"
        role="alert"
        data-testid="data-error"
      >
        <span>{{ error }}</span>
      </div>
      <div
        v-if="notice"
        class="alert alert-success text-sm py-2.5 px-4 shadow-sm"
        role="status"
        data-testid="data-notice"
      >
        <Icon icon="lucide:check" class="size-4" aria-hidden="true" />
        <span>{{ notice }}</span>
      </div>

      <div
        class="rounded-xl border border-base-300 bg-base-100/40 p-4 flex flex-col sm:flex-row sm:items-center justify-between gap-4"
      >
        <div class="flex flex-col gap-0.5 max-w-lg">
          <h3 class="font-semibold text-sm">{{ t('data.export.title') }}</h3>
          <p class="text-xs opacity-70 leading-relaxed">{{ t('data.export.help') }}</p>
        </div>
        <button
          class="btn btn-sm btn-outline gap-1.5 shrink-0 self-start sm:self-auto"
          :disabled="busy"
          data-testid="data-export"
          @click="doExport"
        >
          <Icon icon="lucide:download" class="size-3.5" aria-hidden="true" />
          {{ t('data.export.button') }}
        </button>
      </div>

      <div class="rounded-xl border border-base-300 bg-base-100/40 p-4 space-y-3">
        <div class="flex flex-col gap-0.5">
          <h3 class="font-semibold text-sm">{{ t('data.import.title') }}</h3>
          <p class="text-xs opacity-70 leading-relaxed">{{ t('data.import.help') }}</p>
        </div>
        <label class="form-control max-w-sm">
          <span class="label-text text-xs font-medium mb-1">{{ t('data.import.choose') }}</span>
          <input
            type="file"
            accept="application/json,.json"
            class="file-input file-input-bordered file-input-sm w-full"
            data-testid="data-file"
            @change="onFile"
          />
        </label>
        <template v-if="summary">
          <div
            class="rounded-lg bg-base-200/80 p-3 text-xs leading-relaxed space-y-2 border border-base-300"
            data-testid="data-summary"
          >
            <p>
              {{
                t('data.import.summary', {
                  items: summary.items,
                  options: summary.options,
                  versions: summary.rule_versions,
                  checkins: summary.checkins,
                  categories: summary.categories,
                  freezes: summary.freezes,
                })
              }}
            </p>
          </div>
          <label class="label cursor-pointer justify-start gap-2 py-0">
            <input
              v-model="applySettings"
              type="checkbox"
              class="checkbox checkbox-sm checkbox-primary"
              data-testid="data-apply-settings"
            />
            <span class="label-text text-xs">{{ t('data.import.applySettings') }}</span>
          </label>
          <button
            class="btn btn-sm btn-primary gap-1.5 self-start"
            :disabled="busy"
            data-testid="data-import"
            @click="doImport"
          >
            <Icon icon="lucide:upload" class="size-3.5" aria-hidden="true" />
            {{ t('data.import.button') }}
          </button>
        </template>
      </div>

      <!-- Danger Zone -->
      <div class="rounded-xl border border-error/30 bg-error/5 p-5 space-y-3">
        <div class="flex items-center gap-2 text-error">
          <Icon icon="lucide:triangle-alert" class="size-4.5 shrink-0" aria-hidden="true" />
          <h3 class="font-bold text-sm">{{ t('data.delete.title') }}</h3>
        </div>
        <p class="text-xs opacity-80 leading-relaxed max-w-xl">{{ t('data.delete.help') }}</p>
        <div class="flex flex-col sm:flex-row sm:items-end gap-3 pt-2">
          <div class="flex flex-col gap-1.5 sm:w-60">
            <span class="text-xs font-medium text-error">
              {{ t('data.delete.prompt', { word: CONFIRM_WORD }) }}
            </span>
            <input
              v-model="confirmText"
              class="input input-bordered input-sm w-full font-mono"
              autocomplete="off"
              data-testid="data-confirm"
              :placeholder="CONFIRM_WORD"
            />
          </div>
          <button
            class="btn btn-sm btn-error gap-1.5"
            :disabled="!canDelete"
            data-testid="data-delete"
            @click="doDelete"
          >
            <Icon icon="lucide:trash-2" class="size-3.5" aria-hidden="true" />
            {{ t('data.delete.button') }}
          </button>
        </div>
      </div>
    </div>
  </section>
</template>
