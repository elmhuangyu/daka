<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
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
  <section class="card bg-base-200" data-testid="section-data">
    <div class="card-body gap-4">
      <h2 class="card-title">{{ t('data.title') }}</h2>
      <p class="text-sm opacity-80">{{ t('data.intro') }}</p>
      <p v-if="error" class="text-error" role="alert" data-testid="data-error">{{ error }}</p>
      <p v-if="notice" class="text-success" role="status" data-testid="data-notice">{{ notice }}</p>

      <div class="flex flex-col gap-2">
        <h3 class="font-semibold">{{ t('data.export.title') }}</h3>
        <p class="text-sm opacity-80">{{ t('data.export.help') }}</p>
        <button class="btn btn-sm btn-outline self-start" :disabled="busy" data-testid="data-export" @click="doExport">
          {{ t('data.export.button') }}
        </button>
      </div>

      <div class="flex flex-col gap-2">
        <h3 class="font-semibold">{{ t('data.import.title') }}</h3>
        <p class="text-sm opacity-80">{{ t('data.import.help') }}</p>
        <label class="form-control">
          <span class="label-text">{{ t('data.import.choose') }}</span>
          <input type="file" accept="application/json,.json" class="file-input file-input-bordered file-input-sm" data-testid="data-file" @change="onFile" />
        </label>
        <template v-if="summary">
          <p class="text-sm" data-testid="data-summary">
            {{ t('data.import.summary', {
              items: summary.items, options: summary.options, versions: summary.rule_versions,
              checkins: summary.checkins, categories: summary.categories, freezes: summary.freezes,
            }) }}
          </p>
          <label class="label cursor-pointer justify-start gap-2">
            <input v-model="applySettings" type="checkbox" class="checkbox checkbox-sm" data-testid="data-apply-settings" />
            <span class="label-text">{{ t('data.import.applySettings') }}</span>
          </label>
          <button class="btn btn-sm btn-primary self-start" :disabled="busy" data-testid="data-import" @click="doImport">
            {{ t('data.import.button') }}
          </button>
        </template>
      </div>

      <div class="flex flex-col gap-2">
        <h3 class="font-semibold text-error">{{ t('data.delete.title') }}</h3>
        <p class="text-sm opacity-80">{{ t('data.delete.help') }}</p>
        <label class="form-control">
          <span class="label-text">{{ t('data.delete.prompt', { word: CONFIRM_WORD }) }}</span>
          <input v-model="confirmText" class="input input-bordered input-sm w-48" autocomplete="off" data-testid="data-confirm" />
        </label>
        <button class="btn btn-sm btn-error self-start" :disabled="!canDelete" data-testid="data-delete" @click="doDelete">
          {{ t('data.delete.button') }}
        </button>
      </div>
    </div>
  </section>
</template>
