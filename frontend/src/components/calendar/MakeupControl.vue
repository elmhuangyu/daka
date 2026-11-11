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
const isOccurrence = computed(() => d.value.tracker_type === 'boolean' && negative.value)
const rows = computed(() => valueRows(d.value, props.logs))
const checked = computed(() => props.logs.length > 0)
const lastLogId = computed(() => props.logs.reduce((m, l) => Math.max(m, l.id), 0))

const inputs = reactive<Record<string, string>>({})
const localError = ref('')
const id = computed(() => `mk-${d.value.id}`)

function parse(s: unknown): number | null {
  if (s == null || String(s).trim() === '') return null
  const n = Number(s)
  return Number.isFinite(n) ? n : null
}

function occurrence() {
  emit('log', { item_id: d.value.id, mode: 'append' })
}

function bump(r: ValueRow, sign: 1 | -1) {
  const amount = sign === 1 ? r.step : Math.min(r.step, r.current ?? 0)
  if (amount <= 0) return
  const opt = r.optionId == null ? {} : { option_id: r.optionId }
  emit('log', { item_id: d.value.id, mode: 'delta', value: sign * amount, ...opt })
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
    emit('log', { item_id: d.value.id, mode: 'delta', value: delta, ...opt })
  } else {
    emit('log', { item_id: d.value.id, mode: 'append', value: n, ...opt })
  }
  inputs[r.key] = ''
}
</script>

<template>
  <div class="space-y-2" :aria-busy="busy" :data-testid="`makeup-${d.id}`">
    <div v-if="isOccurrence" class="flex flex-wrap items-center gap-2">
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
      <div
        v-for="r in rows"
        :key="r.key"
        class="flex flex-wrap items-center gap-x-3 gap-y-1"
        :data-testid="`makeup-row-${d.id}-${r.key}`"
      >
        <template v-if="d.tracker_type === 'counter'">
          <span v-if="r.label" class="text-sm font-medium">{{ r.label }}</span>
          <span
            class="text-sm font-semibold tabular-nums"
            :class="
              r.target !== null && (r.current ?? 0) >= r.target ? 'text-success' : 'text-primary'
            "
          >
            {{ fmt(r.current ?? 0)
            }}<template v-if="r.target !== null"> / {{ fmt(r.target) }}</template>
            {{ r.unit }}
          </span>
          <progress
            v-if="r.target"
            class="progress min-w-12 flex-1"
            :class="(r.current ?? 0) >= r.target ? 'progress-success' : 'progress-primary'"
            :value="Math.min(r.current ?? 0, r.target)"
            :max="r.target"
          ></progress>
          <div class="ml-auto flex items-center gap-1.5">
            <button
              type="button"
              class="btn btn-sm"
              :disabled="busy || (r.current ?? 0) <= 0"
              :data-testid="`makeup-dec-${d.id}-${r.key}`"
              @click="bump(r, -1)"
            >
              − {{ fmt(r.step) }} {{ r.unit }}
            </button>
            <button
              type="button"
              class="btn btn-sm btn-primary"
              :disabled="busy"
              :data-testid="`makeup-inc-${d.id}-${r.key}`"
              @click="bump(r, 1)"
            >
              + {{ fmt(r.step) }} {{ r.unit }}
            </button>
          </div>
        </template>
        <template v-else>
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
            {{ t('calendar.makeup.record') }}
          </button>
        </template>
      </div>
    </template>
    <p v-if="localError" class="text-sm text-error" role="alert">{{ localError }}</p>
  </div>
</template>
