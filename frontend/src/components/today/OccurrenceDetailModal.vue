<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { Icon } from '@iconify/vue'
import type { CheckinInput, CheckinLog, ItemDetail } from '../../api'
import { formatCustomDate } from '../../i18n/format'
import { useI18n } from '../../i18n'
import { adhocLogs, fmt, valueRows, type ValueRow } from '../../today/logic'

const { t } = useI18n()

const props = defineProps<{
  open: boolean
  detail: ItemDetail | null
  logs: CheckinLog[]
  date: string
  busy: boolean
  redact: boolean
}>()

const emit = defineEmits<{
  close: []
  log: [CheckinInput]
  remove: [number]
  patchNote: [id: number, note: string]
  setDueDate: [itemId: number, dueDate: string | null]
}>()

const isToggle = computed(() => {
  if (!props.detail) return false
  return (
    (props.detail.tracker_type === 'boolean' && props.detail.polarity !== 'negative') ||
    props.detail.tracker_type === 'maintenance'
  )
})

const rows = computed(() => (props.detail ? valueRows(props.detail, props.logs) : []))
const adhoc = computed(() => (props.detail ? adhocLogs(props.detail, props.logs) : []))
const hasOptions = computed(() => (props.detail ? props.detail.options.length > 0 : false))

const toggleNote = ref('')
const todoDate = ref('')
const logNotes = reactive<Record<number, string>>({})
const inputs = reactive<Record<string, string>>({})
const adhocName = ref('')
const adhocValue = ref('')
const localError = ref('')

function syncState() {
  localError.value = ''
  toggleNote.value = ''
  for (const k of Object.keys(logNotes)) delete logNotes[Number(k)]
  todoDate.value = ''
  if (!props.open || !props.detail) return
  todoDate.value =
    (props.detail.current_rule.schedule_type === 'fixed_rrule'
      ? props.detail.current_rule.dtstart
      : '') || ''
  toggleNote.value =
    [...props.logs]
      .reverse()
      .find((l) => l.note && l.note.trim())
      ?.note?.trim() ?? ''
  for (const l of props.logs) {
    logNotes[l.id] = l.note ?? ''
  }
}

watch(
  () => [props.open, props.detail?.id, props.date, props.logs],
  () => {
    syncState()
  },
  { immediate: true, deep: true },
)

function parse(s: unknown): number | null {
  if (s == null || String(s).trim() === '') return null
  const n = Number(s)
  return Number.isFinite(n) ? n : null
}

function optionPart(r: ValueRow): { option_id?: number } {
  return r.optionId == null ? {} : { option_id: r.optionId }
}

function saveToggleNote() {
  if (!props.detail) return
  const n = toggleNote.value.trim()
  const latestLogWithNote = [...props.logs].reverse().find((l) => l.note && l.note.trim())
  const targetLog = latestLogWithNote ?? props.logs[props.logs.length - 1] ?? props.logs[0]
  if (targetLog) {
    emit('patchNote', targetLog.id, n)
  } else {
    emit('log', {
      item_id: props.detail.id,
      mode: 'set',
      value: 1,
      ...(n ? { note: n } : {}),
    })
  }
}

function saveDueDate() {
  if (props.detail) {
    emit('setDueDate', props.detail.id, todoDate.value || null)
  }
}

function clearDueDate() {
  todoDate.value = ''
  if (props.detail) {
    emit('setDueDate', props.detail.id, null)
  }
}

function setValue(r: ValueRow) {
  if (!props.detail) return
  const n = parse(inputs[r.key])
  if (n === null || n < 0) {
    localError.value = t('today.card.err.valueMin')
    return
  }
  const delta = n - (r.current ?? 0)
  if (Math.abs(delta) < 1e-9) return
  localError.value = ''
  emit('log', {
    item_id: props.detail.id,
    mode: 'delta',
    value: delta,
    ...optionPart(r),
  })
  inputs[r.key] = ''
}

function addAdhoc() {
  if (!props.detail) return
  const name = adhocName.value.trim()
  const n = parse(adhocValue.value)
  if (!name || n === null) {
    localError.value = t('today.card.err.adhoc')
    return
  }
  localError.value = ''
  emit('log', {
    item_id: props.detail.id,
    mode: props.detail.tracker_type === 'counter' ? 'delta' : 'append',
    value: n,
    adhoc: true,
    note: name,
  })
  adhocName.value = ''
  adhocValue.value = ''
}

function optionForLog(l: CheckinLog) {
  if (l.option_id == null || !props.detail) return null
  return props.detail.options.find((o) => o.id === l.option_id) ?? null
}

function logLabel(l: CheckinLog): string {
  const opt = optionForLog(l)
  if (opt) return opt.name
  if (l.option_id == null && props.detail && props.detail.options.length > 0) {
    return l.note || t('today.card.adhocLabel')
  }
  return ''
}

function logUnit(l: CheckinLog): string {
  const opt = optionForLog(l)
  if (opt) return opt.unit
  return props.detail?.unit ?? ''
}
</script>

<template>
  <div
    v-if="open && detail"
    class="modal modal-open"
    role="dialog"
    aria-modal="true"
    data-testid="occurrence-modal"
    @keydown.escape="emit('close')"
  >
    <div class="modal-box relative max-w-lg space-y-4">
      <!-- Header -->
      <header class="flex items-start justify-between gap-3 border-b border-base-200 pb-3">
        <div>
          <div class="flex items-center gap-2">
            <span
              v-if="detail.color"
              class="size-3 rounded-full shrink-0"
              :style="{ backgroundColor: detail.color }"
            />
            <h3 class="name text-lg font-bold" :class="{ sensitive: detail.effective_sensitive }">
              {{ detail.name }}
            </h3>
          </div>
          <p class="text-xs text-base-content/60 font-mono mt-0.5">
            {{ formatCustomDate(date) }}
          </p>
        </div>
        <div class="flex items-center gap-1.5 shrink-0">
          <RouterLink
            v-if="detail.kind !== 'event'"
            :to="`/items/${detail.id}`"
            class="btn btn-ghost btn-xs gap-1"
            data-testid="modal-item-link"
          >
            <Icon icon="lucide:sliders-horizontal" class="size-3.5" aria-hidden="true" />
            <span>{{ t('today.modal.editHabit') }}</span>
          </RouterLink>
          <button
            type="button"
            class="btn btn-ghost btn-sm btn-square"
            data-testid="modal-close"
            :aria-label="t('common.close')"
            @click="emit('close')"
          >
            <Icon icon="lucide:x" class="size-4" aria-hidden="true" />
          </button>
        </div>
      </header>

      <!-- Redact warning notice -->
      <div
        v-if="redact"
        class="alert alert-warning text-xs shadow-xs"
        role="status"
        data-testid="modal-redact-notice"
      >
        <Icon icon="lucide:lock" class="size-4 shrink-0" aria-hidden="true" />
        <span>{{ t('today.modal.redactNotice') }}</span>
      </div>

      <!-- Occurrence Note / Details section (when !redact) -->
      <div v-else class="space-y-3">
        <!-- isToggle: textarea and save button -->
        <div v-if="isToggle" class="space-y-2">
          <label class="text-sm font-medium block">
            {{ t('today.modal.noteLabel') }}
          </label>
          <textarea
            v-model="toggleNote"
            class="textarea textarea-bordered w-full text-sm resize-y"
            rows="3"
            maxlength="2000"
            :placeholder="t('today.modal.notePlaceholder')"
            data-testid="occurrence-note-input"
          ></textarea>
          <div class="flex justify-end">
            <button
              type="button"
              class="btn btn-primary btn-sm"
              :disabled="busy"
              data-testid="save-occurrence-note"
              @click="saveToggleNote"
            >
              {{ t('today.modal.saveNote') }}
            </button>
          </div>
        </div>

        <!-- counter, metric, and negative boolean -->
        <div v-else class="space-y-2">
          <div v-if="logs.length > 0" class="space-y-2" data-testid="modal-logs">
            <div
              v-for="l in logs"
              :key="l.id"
              class="p-2.5 rounded-lg border border-base-200 bg-base-200/30 space-y-2"
            >
              <div class="flex items-center justify-between gap-2">
                <div class="flex items-center gap-1.5 text-sm font-medium">
                  <span>{{ fmt(l.value) }}</span>
                  <span v-if="logUnit(l)" class="text-xs opacity-70">{{ logUnit(l) }}</span>
                  <span v-if="logLabel(l)" class="badge badge-sm badge-neutral">
                    {{ logLabel(l) }}
                  </span>
                </div>
                <button
                  type="button"
                  class="btn btn-ghost btn-xs btn-square text-error"
                  :disabled="busy"
                  :aria-label="t('today.card.removeEntry')"
                  :data-testid="`modal-remove-${l.id}`"
                  @click="emit('remove', l.id)"
                >
                  <Icon icon="lucide:trash-2" class="size-3.5" aria-hidden="true" />
                </button>
              </div>
              <div class="flex items-center gap-2">
                <input
                  v-model="logNotes[l.id]"
                  type="text"
                  maxlength="2000"
                  class="input input-sm input-bordered flex-1"
                  :placeholder="t('today.modal.logNotePlaceholder')"
                  :data-testid="`log-note-input-${l.id}`"
                  @keydown.enter.prevent="emit('patchNote', l.id, logNotes[l.id] ?? '')"
                />
                <button
                  type="button"
                  class="btn btn-sm btn-ghost"
                  :disabled="busy"
                  :data-testid="`save-log-note-${l.id}`"
                  @click="emit('patchNote', l.id, logNotes[l.id] ?? '')"
                >
                  {{ t('common.save') }}
                </button>
              </div>
            </div>
          </div>
          <p v-else class="text-sm opacity-60">
            {{ t('today.modal.noLogs') }}
          </p>
        </div>
      </div>

      <!-- Due Date / Schedule section for Todos (KindEvent) -->
      <section
        v-if="detail.kind === 'event'"
        class="border-t border-base-200 pt-3 space-y-2"
        data-testid="modal-todo-date-section"
      >
        <h4
          class="text-xs font-semibold uppercase tracking-wider text-base-content/60 flex items-center gap-1.5"
        >
          <Icon icon="lucide:calendar" class="size-3.5" />
          {{ t('today.modal.dueDateSection') }}
        </h4>
        <div class="flex flex-wrap items-center gap-2">
          <input
            v-model="todoDate"
            type="date"
            class="input input-sm input-bordered font-mono"
            data-testid="modal-todo-date-input"
          />
          <button
            type="button"
            class="btn btn-sm btn-primary"
            :disabled="busy"
            data-testid="modal-todo-date-save"
            @click="saveDueDate"
          >
            {{ t('common.save') }}
          </button>
          <button
            v-if="detail.current_rule.schedule_type === 'fixed_rrule' || todoDate"
            type="button"
            class="btn btn-sm btn-ghost text-base-content/70"
            :disabled="busy"
            data-testid="modal-todo-date-clear"
            @click="clearDueDate"
          >
            {{ t('today.modal.makeUndated') }}
          </button>
        </div>
      </section>

      <!-- Secondary Controls moved from ItemCard.vue -->
      <section
        v-if="
          detail.tracker_type === 'counter' ||
          (hasOptions && (detail.tracker_type === 'counter' || detail.tracker_type === 'metric'))
        "
        class="border-t border-base-200 pt-3 space-y-3"
      >
        <!-- Exact total setter for counter rows -->
        <div v-if="detail.tracker_type === 'counter'" class="space-y-2">
          <h4 class="text-xs font-semibold uppercase tracking-wider text-base-content/60">
            {{ t('today.modal.adjustTotal') }}
          </h4>
          <div
            v-for="r in rows"
            :key="r.key"
            class="flex flex-wrap items-center gap-2"
            :data-testid="`modal-row-${r.key}`"
          >
            <span
              v-if="r.label"
              class="name text-sm font-medium"
              :class="{ sensitive: detail.effective_sensitive }"
            >
              {{ r.label }}:
            </span>
            <span class="text-sm opacity-70">
              {{ fmt(r.current ?? 0)
              }}<template v-if="r.target !== null"> / {{ fmt(r.target) }}</template> {{ r.unit }}
            </span>
            <input
              v-model="inputs[r.key]"
              type="number"
              inputmode="decimal"
              min="0"
              step="any"
              class="input input-sm input-bordered w-24"
              :aria-label="
                r.label ? t('today.card.setTotalFor', { label: r.label }) : t('today.card.setTotal')
              "
              :placeholder="t('today.card.setTo')"
              data-testid="set-input"
              @keydown.enter.prevent="setValue(r)"
            />
            <button
              type="button"
              class="btn btn-sm"
              :disabled="busy"
              data-testid="set"
              @click="setValue(r)"
            >
              {{ t('today.card.set') }}
            </button>
          </div>
        </div>

        <!-- Ad-hoc option form -->
        <div
          v-if="
            hasOptions && (detail.tracker_type === 'counter' || detail.tracker_type === 'metric')
          "
          class="space-y-2"
        >
          <form
            class="flex flex-wrap items-center gap-2"
            data-testid="adhoc-form"
            @submit.prevent="addAdhoc"
          >
            <input
              v-model="adhocName"
              class="input input-sm input-bordered min-w-0 flex-1"
              :aria-label="t('today.card.adhocName')"
              :placeholder="t('today.card.adhocNamePlaceholder')"
              maxlength="100"
            />
            <input
              v-model="adhocValue"
              type="number"
              inputmode="decimal"
              step="any"
              class="input input-sm input-bordered w-24"
              :aria-label="t('today.card.adhocValue')"
              :placeholder="t('today.card.value')"
            />
            <button type="submit" class="btn btn-sm" :disabled="busy" data-testid="adhoc-add">
              {{ t('common.add') }}
            </button>
          </form>
          <ul v-if="adhoc.length && detail.tracker_type === 'counter'" class="text-sm opacity-80">
            <li v-for="l in adhoc" :key="l.id">
              {{ l.note || t('today.card.adhocTitle') }}: {{ fmt(l.value) }}
            </li>
          </ul>
        </div>
      </section>

      <p v-if="localError" class="error text-sm text-error" role="alert">{{ localError }}</p>
    </div>
    <div class="modal-backdrop" @click="emit('close')">
      <button type="button" class="cursor-default" :aria-label="t('common.close')"></button>
    </div>
  </div>
</template>
