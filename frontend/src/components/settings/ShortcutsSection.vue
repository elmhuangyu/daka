<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n, type MessageKey } from '../../i18n'
import { Icon } from '@iconify/vue'
import {
  PRIVACY_TOGGLE_ID,
  SHORTCUT_CATALOG,
  catalogEntry,
  type CatalogEntry,
} from '../../composables/shortcutCatalog'
import {
  bindingKeyParts,
  canonicalBinding,
  validateBinding,
} from '../../composables/shortcutBindings'
import {
  applyPrefs,
  catalogConflicts,
  effectiveMap,
  parseShortcutPrefs,
  replaceBindings,
  resetAllBindings,
  resetBinding,
  serializeShortcutPrefs,
  setBinding,
  setShortcutsEnabled,
  shortcutPrefs,
  type BindingOverrides,
} from '../../composables/shortcutPrefs'
import {
  normalizeEvent,
  setShortcutRecording,
  type ShortcutScope,
} from '../../composables/useShortcuts'

// Static text only; this section never reads item, project or category data.
const { t } = useI18n()
const SCOPE_TITLES: Record<ShortcutScope, MessageKey> = {
  global: 'shortcuts.scope.global',
  today: 'shortcuts.scope.today',
  calendar: 'shortcuts.scope.calendar',
}
const SCOPE_ORDER: ShortcutScope[] = ['global', 'today', 'calendar']
const EXPORT_FILE = 'daka-shortcuts.json'
const MAX_IMPORT_BYTES = 64 * 1024

const groups = computed(() =>
  SCOPE_ORDER.map((scope) => ({
    scope,
    title: t(SCOPE_TITLES[scope]),
    rows: SHORTCUT_CATALOG.filter((e) => e.scope === scope),
  })).filter((g) => g.rows.length > 0),
)

const map = computed(() => effectiveMap())
const keysOf = (id: string): string[][] => (map.value[id] ?? []).map(bindingKeyParts)
const isOverridden = (id: string) => shortcutPrefs.bindings[id] !== undefined
const canUnbind = (e: CatalogEntry) =>
  !e.displayOnly && e.id !== PRIVACY_TOGGLE_ID && (map.value[e.id] ?? []).length > 0
const labelOf = (id: string) => {
  const e = catalogEntry(id)
  return e ? t(e.labelKey) : id
}

// --- record mode ---------------------------------------------------------
const SEQUENCE_MS = 1000
const MODIFIERS = new Set(['control', 'alt', 'shift', 'meta'])
const recordingId = ref<string | null>(null)
const steps = ref<string[]>([])
const error = ref('')
const notice = ref('')
const pendingSwap = ref<{ id: string; binding: string; others: string[] } | null>(null)
let timer: ReturnType<typeof setTimeout> | null = null

function clearTimer() {
  if (timer !== null) clearTimeout(timer)
  timer = null
}

function stopRecording() {
  clearTimer()
  window.removeEventListener('keydown', onRecordKey, true)
  recordingId.value = null
  steps.value = []
  // Keep dispatch suspended until the key that ended recording has finished propagating.
  setTimeout(() => setShortcutRecording(false), 0)
}

function startRecording(id: string) {
  if (recordingId.value !== null) stopRecording()
  error.value = ''
  notice.value = ''
  pendingSwap.value = null
  recordingId.value = id
  steps.value = []
  setShortcutRecording(true)
  window.addEventListener('keydown', onRecordKey, true)
}

function onRecordKey(e: KeyboardEvent) {
  e.preventDefault()
  e.stopImmediatePropagation()
  if (e.isComposing || e.repeat || MODIFIERS.has(e.key.toLowerCase())) return
  if (e.key === 'Escape') {
    stopRecording()
    return
  }
  const chord = normalizeEvent(e)
  steps.value = [...steps.value, chord]
  const plain = !/(^|\+)(ctrl|alt|meta)\+/.test(chord)
  if (steps.value.length === 1 && plain) {
    clearTimer()
    timer = setTimeout(finishRecording, SEQUENCE_MS)
    return
  }
  finishRecording()
}

function finishRecording() {
  const id = recordingId.value
  const candidate = steps.value.join(' ')
  stopRecording()
  if (id !== null) propose(id, candidate)
}

function currentOverrides(): BindingOverrides {
  return { ...shortcutPrefs.bindings }
}

function propose(id: string, candidate: string) {
  const invalid = validateBinding(candidate)
  if (invalid) {
    error.value = t(invalid)
    return
  }
  const binding = canonicalBinding(candidate)
  const next = currentOverrides()
  next[id] = [binding]
  const others = catalogConflicts(next)
    .filter((c) => c.a === id || c.b === id)
    .map((c) => (c.a === id ? c.b : c.a))
  if (others.length > 0) {
    pendingSwap.value = { id, binding, others: [...new Set(others)] }
    return
  }
  commit(id, binding)
}

function commit(id: string, binding: string) {
  const def = catalogEntry(id)
  const isDefault =
    def !== undefined && def.keys.length === 1 && canonicalBinding(def.keys[0]!) === binding
  if (isDefault) resetBinding(id)
  else setBinding(id, [binding])
}

function swap() {
  const p = pendingSwap.value
  if (!p) return
  const previous = map.value[p.id] ?? []
  const next = currentOverrides()
  next[p.id] = [p.binding]
  for (const other of p.others) next[other] = previous.length > 0 ? previous : null
  if (next[PRIVACY_TOGGLE_ID] === null || catalogConflicts(next).length > 0) {
    error.value = t('shortcuts.settings.swapFailed')
    pendingSwap.value = null
    return
  }
  replaceBindings(next)
  pendingSwap.value = null
}

function cancelSwap() {
  pendingSwap.value = null
}

function unbind(id: string) {
  error.value = ''
  setBinding(id, null)
}

function reset(id: string) {
  error.value = ''
  pendingSwap.value = null
  resetBinding(id)
}

function resetAll() {
  error.value = ''
  notice.value = ''
  pendingSwap.value = null
  resetAllBindings()
}

function toggleEnabled(e: Event) {
  setShortcutsEnabled((e.target as HTMLInputElement).checked)
}

// --- export / import -----------------------------------------------------
const fileInput = ref<HTMLInputElement | null>(null)

function exportJson() {
  const blob = new Blob([serializeShortcutPrefs()], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = EXPORT_FILE
  document.body.append(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

function readText(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const r = new FileReader()
    r.onload = () => resolve(String(r.result))
    r.onerror = () => reject(r.error)
    r.readAsText(file)
  })
}

async function onImport(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  error.value = ''
  notice.value = ''
  if (!file) return
  if (file.size > MAX_IMPORT_BYTES) {
    error.value = t('shortcuts.settings.tooLarge')
    return
  }
  let text: string
  try {
    text = await readText(file)
  } catch {
    error.value = t('shortcuts.settings.readFailed')
    return
  }
  const known = new Set(SHORTCUT_CATALOG.filter((c) => !c.displayOnly).map((c) => c.id))
  const parsed = parseShortcutPrefs(text, known)
  if (!parsed.ok) {
    error.value = t(parsed.error)
    return
  }
  if (catalogConflicts(parsed.prefs.bindings).length > 0) {
    error.value = t('shortcuts.settings.importConflict')
    return
  }
  applyPrefs(parsed.prefs)
  notice.value =
    parsed.ignored > 0
      ? t('shortcuts.settings.importedIgnored', { count: parsed.ignored })
      : t('shortcuts.settings.imported')
}

onBeforeUnmount(() => {
  if (recordingId.value !== null) stopRecording()
})
</script>

<template>
  <section
    class="card bg-base-200/50 border border-base-300 shadow-sm"
    data-testid="section-shortcuts"
  >
    <div class="card-body p-6 gap-6">
      <div class="flex items-center gap-3 border-b border-base-300/80 pb-4">
        <div class="flex size-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
          <Icon icon="lucide:keyboard" class="size-5" aria-hidden="true" />
        </div>
        <div>
          <h2 class="text-lg font-bold leading-none">{{ t('shortcuts.title') }}</h2>
          <p class="mt-1 text-xs opacity-70">{{ t('shortcuts.settings.intro') }}</p>
        </div>
      </div>

      <label
        class="rounded-xl border border-base-300 bg-base-100/50 p-4 flex items-center justify-between gap-4 cursor-pointer"
      >
        <div class="flex flex-col gap-0.5">
          <span class="text-sm font-medium">{{ t('shortcuts.settings.enable') }}</span>
          <span class="text-[11px] opacity-60">{{ t('shortcuts.settings.enableHint') }}</span>
        </div>
        <input
          type="checkbox"
          class="checkbox checkbox-primary checkbox-sm"
          :checked="shortcutPrefs.enabled"
          data-testid="sc-enabled"
          @change="toggleEnabled"
        />
      </label>

      <div
        v-if="error"
        class="alert alert-error text-sm py-2.5 px-4"
        role="alert"
        data-testid="sc-error"
      >
        <span>{{ error }}</span>
      </div>
      <div
        v-if="notice"
        class="alert alert-success text-sm py-2.5 px-4"
        role="status"
        data-testid="sc-notice"
      >
        <span>{{ notice }}</span>
      </div>
      <div
        v-if="pendingSwap"
        class="alert alert-warning text-sm py-2.5 px-4 flex-wrap"
        role="alert"
        data-testid="sc-conflict"
      >
        <span>
          <kbd class="kbd kbd-sm">{{ pendingSwap.binding }}</kbd>
          {{ t('shortcuts.settings.conflictsWith') }}
          {{ pendingSwap.others.map(labelOf).join(', ') }}
        </span>
        <div class="flex gap-2">
          <button type="button" class="btn btn-sm" data-testid="sc-swap" @click="swap">
            {{ t('shortcuts.settings.swap') }}
          </button>
          <button
            type="button"
            class="btn btn-sm btn-ghost"
            data-testid="sc-swap-cancel"
            @click="cancelSwap"
          >
            {{ t('common.cancel') }}
          </button>
        </div>
      </div>

      <div v-for="g in groups" :key="g.scope" :data-testid="`sc-group-${g.scope}`">
        <h3 class="text-sm font-semibold text-base-content/70">{{ g.title }}</h3>
        <ul class="mt-2 divide-y divide-base-300/60">
          <li
            v-for="r in g.rows"
            :key="r.id"
            class="flex flex-wrap items-center justify-between gap-2 py-2 text-sm"
            :data-testid="`sc-row-${r.id}`"
          >
            <span>{{ t(r.labelKey) }}</span>
            <span class="flex flex-wrap items-center gap-2">
              <template v-if="recordingId === r.id">
                <span
                  :id="`sc-hint-${r.id}`"
                  class="text-xs opacity-70"
                  role="status"
                  aria-live="polite"
                  data-testid="sc-recording"
                >
                  {{
                    steps.length === 0
                      ? t('shortcuts.settings.press')
                      : t('shortcuts.settings.pressMore')
                  }}
                </span>
                <kbd v-for="(s, i) in steps" :key="i" class="kbd kbd-sm">{{ s }}</kbd>
              </template>
              <template v-else>
                <span v-if="keysOf(r.id).length === 0" class="opacity-60" data-testid="sc-unbound">
                  {{ t('shortcuts.unbound') }}
                </span>
                <span
                  v-for="(seq, i) in keysOf(r.id)"
                  :key="i"
                  class="flex gap-1"
                  data-testid="sc-keys"
                >
                  <kbd v-for="(k, j) in seq" :key="j" class="kbd kbd-sm">{{ k }}</kbd>
                </span>
              </template>
              <span v-if="r.displayOnly" class="text-xs opacity-60">{{
                t('shortcuts.fixed')
              }}</span>
              <template v-else>
                <button
                  type="button"
                  class="btn btn-xs"
                  :aria-pressed="recordingId === r.id"
                  :aria-describedby="recordingId === r.id ? `sc-hint-${r.id}` : undefined"
                  :data-testid="`sc-change-${r.id}`"
                  @click="startRecording(r.id)"
                >
                  {{ t('shortcuts.settings.change') }}
                </button>
                <button
                  type="button"
                  class="btn btn-xs btn-ghost"
                  :disabled="!isOverridden(r.id)"
                  :data-testid="`sc-reset-${r.id}`"
                  @click="reset(r.id)"
                >
                  {{ t('shortcuts.settings.reset') }}
                </button>
                <button
                  v-if="r.id !== PRIVACY_TOGGLE_ID"
                  type="button"
                  class="btn btn-xs btn-ghost"
                  :disabled="!canUnbind(r)"
                  :data-testid="`sc-unbind-${r.id}`"
                  @click="unbind(r.id)"
                >
                  {{ t('shortcuts.settings.unbind') }}
                </button>
              </template>
            </span>
          </li>
        </ul>
      </div>

      <div class="flex flex-wrap items-center gap-2 border-t border-base-300/80 pt-4">
        <button type="button" class="btn btn-sm" data-testid="sc-reset-all" @click="resetAll">
          {{ t('shortcuts.settings.resetAll') }}
        </button>
        <button type="button" class="btn btn-sm" data-testid="sc-export" @click="exportJson">
          {{ t('shortcuts.settings.export') }}
        </button>
        <button
          type="button"
          class="btn btn-sm"
          data-testid="sc-import"
          @click="fileInput?.click()"
        >
          {{ t('shortcuts.settings.import') }}
        </button>
        <input
          ref="fileInput"
          type="file"
          accept="application/json,.json"
          class="hidden"
          data-testid="sc-import-input"
          @change="onImport"
        />
      </div>
    </div>
  </section>
</template>
