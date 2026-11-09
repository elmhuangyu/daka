<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import type { CheckinInput, CheckinLog, ItemDetail } from '../../api'
import { fmt, valueRows, type ValueRow } from '../../today/logic'
import { useI18n } from '../../i18n'

const { t } = useI18n()

const props = defineProps<{ detail: ItemDetail; logs: CheckinLog[]; busy: boolean; date: string }>()
const emit = defineEmits<{ log: [Omit<CheckinInput, 'date'>]; remove: [number] }>()

const d = computed(() => props.detail)
const negative = computed(() => d.value.polarity === 'negative')
const isToggle = computed(
  () =>
    (d.value.tracker_type === 'boolean' && !negative.value) ||
    d.value.tracker_type === 'maintenance',
)
const isOccurrence = computed(() => d.value.tracker_type === 'boolean' && negative.value)
const rows = computed(() => valueRows(d.value, props.logs))
const checked = computed(() => props.logs.length > 0)
const lastLogId = computed(() => props.logs.reduce((m, l) => Math.max(m, l.id), 0))

const note = ref('')
const inputs = reactive<Record<string, string>>({})
const localError = ref('')
const id = computed(() => `mk-${d.value.id}`)

function parse(s: unknown): number | null {
  if (s == null || String(s).trim() === '') return null
  const n = Number(s)
  return Number.isFinite(n) ? n : null
}
const noteField = () => (note.value.trim() ? { note: note.value.trim() } : {})

function toggle() {
  if (checked.value) emit('log', { item_id: d.value.id, mode: 'unset' })
  else emit('log', { item_id: d.value.id, mode: 'set', ...noteField() })
  note.value = ''
}

function occurrence() {
  emit('log', { item_id: d.value.id, mode: 'append', ...noteField() })
  note.value = ''
}

function setValue(r: ValueRow) {
  const n = parse(inputs[r.key])
  if (n === null || n < 0) {
    localError.value = t('today.card.err.valueMin')
    return
  }
  if ((r.min !== null && n < r.min) || (r.max !== null && n > r.max)) {
    localError.value = t('today.card.err.range', { min: r.min ?? '-∞', max: r.max ?? '∞' })
    return
  }
  localError.value = ''
  const opt = r.optionId == null ? {} : { option_id: r.optionId }
  if (d.value.tracker_type === 'counter') {
    const delta = n - (r.current ?? 0)
    if (Math.abs(delta) < 1e-9) return
    emit('log', { item_id: d.value.id, mode: 'delta', value: delta, ...opt, ...noteField() })
  } else {
    emit('log', { item_id: d.value.id, mode: 'append', value: n, ...opt, ...noteField() })
  }
  inputs[r.key] = ''
  note.value = ''
}
</script>

<template>
  <div class="space-y-2" :aria-busy="busy" :data-testid="`makeup-${d.id}`">
    <div v-if="isToggle" class="flex flex-wrap items-center gap-2">
      <button
        type="button"
        class="btn btn-sm"
        :class="checked ? 'btn-ghost' : 'btn-primary'"
        :disabled="busy"
        :data-testid="`makeup-toggle-${d.id}`"
        @click="toggle"
      >
        {{ checked ? t('calendar.makeup.undo') : t('calendar.makeup.markDone') }}
      </button>
    </div>

    <div v-else-if="isOccurrence" class="flex flex-wrap items-center gap-2">
      <button type="button" class="btn btn-sm btn-primary" :disabled="busy" @click="occurrence">
        {{ t('calendar.makeup.addOccurrence') }}
      </button>
      <span class="text-sm opacity-70">{{
        t('calendar.makeup.recorded', { count: logs.length })
      }}</span>
      <button
        v-if="checked"
        type="button"
        class="btn btn-sm btn-ghost"
        :disabled="busy"
        @click="emit('remove', lastLogId)"
      >
        {{ t('calendar.makeup.removeLast') }}
      </button>
    </div>

    <template v-else>
      <div v-for="r in rows" :key="r.key" class="flex flex-wrap items-center gap-2">
        <span class="min-w-16 text-sm">{{
          t('calendar.makeup.rowLine', {
            label: r.label || t('today.card.value'),
            value: r.current == null ? '-' : fmt(r.current),
            unit: r.unit,
          })
        }}</span>
        <label class="sr-only" :for="`${id}-${r.key}`">{{
          t('calendar.makeup.newValue', { label: r.label || d.name })
        }}</label>
        <input
          :id="`${id}-${r.key}`"
          v-model="inputs[r.key]"
          type="number"
          :step="r.step"
          inputmode="decimal"
          class="input input-sm w-24"
          :data-testid="`makeup-input-${d.id}-${r.key}`"
          @keydown.enter.prevent="setValue(r)"
        />
        <button
          type="button"
          class="btn btn-sm btn-primary"
          :disabled="busy"
          :data-testid="`makeup-set-${d.id}-${r.key}`"
          @click="setValue(r)"
        >
          {{
            d.tracker_type === 'counter' ? t('calendar.makeup.set') : t('calendar.makeup.record')
          }}
        </button>
      </div>
    </template>

    <div v-if="!checked || !isToggle" class="flex items-center gap-2">
      <label class="sr-only" :for="`${id}-note`">{{ t('calendar.makeup.noteLabel') }}</label>
      <input
        :id="`${id}-note`"
        v-model="note"
        type="text"
        maxlength="200"
        :placeholder="t('calendar.makeup.notePlaceholder')"
        class="input input-sm w-full"
        :data-testid="`makeup-note-${d.id}`"
      />
    </div>
    <p v-if="localError" class="text-sm text-error" role="alert">{{ localError }}</p>
  </div>
</template>
