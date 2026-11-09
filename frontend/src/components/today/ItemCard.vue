<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { Icon } from '@iconify/vue'
import type { Category, CheckinInput } from '../../api'
import { useI18n } from '../../i18n'
import {
  adhocLogs,
  fmt,
  maintenanceText,
  quotaText,
  valueRows,
  type CardVM,
  type ValueRow,
} from '../../today/logic'

const { t } = useI18n()

const props = defineProps<{
  vm: CardVM
  category: Category | null
  variant: 'todo' | 'done' | 'maintenance' | 'quota'
  busy: boolean
  /** An offline check-in for this item is queued and not yet confirmed. */
  pending?: boolean
}>()
const emit = defineEmits<{ log: [CheckinInput]; remove: [number] }>()

const d = computed(() => props.vm.detail)
const logs = computed(() => props.vm.logs)
const negative = computed(() => d.value.polarity === 'negative')
const rows = computed(() => valueRows(d.value, logs.value))
const adhoc = computed(() => adhocLogs(d.value, logs.value))
const hasOptions = computed(() => d.value.options.length > 0)
const isToggle = computed(
  () =>
    (d.value.tracker_type === 'boolean' && !negative.value) ||
    d.value.tracker_type === 'maintenance',
)
const isOccurrence = computed(() => d.value.tracker_type === 'boolean' && negative.value)
const checked = computed(() => logs.value.length > 0)
const itemId = computed(() => d.value.id)
const lastLogId = computed(() => logs.value.reduce((m, l) => Math.max(m, l.id), 0))

const quota = computed(() => d.value.stats?.quota ?? null)
const trafficClass = computed(() => {
  switch (quota.value?.traffic) {
    case 'red':
      return 'progress-error'
    case 'yellow':
      return 'progress-warning'
    case 'green':
      return 'progress-success'
    default:
      return 'progress-primary'
  }
})

const inputs = reactive<Record<string, string>>({})
const note = ref('')
const adhocName = ref('')
const adhocValue = ref('')
const localError = ref('')

// v-model on type=number inputs yields a number (or '' when empty).
function parse(s: unknown): number | null {
  if (s == null || String(s).trim() === '') return null
  const n = Number(s)
  return Number.isFinite(n) ? n : null
}

function valueAria(r: ValueRow): string {
  if (r.label && r.unit) return t('today.card.valueForIn', { label: r.label, unit: r.unit })
  if (r.label) return t('today.card.valueFor', { label: r.label })
  if (r.unit) return t('today.card.valueIn', { unit: r.unit })
  return t('today.card.value')
}

function toggle() {
  emit('log', {
    item_id: itemId.value,
    mode: checked.value ? 'unset' : 'set',
    ...(!checked.value && note.value.trim() ? { note: note.value.trim() } : {}),
  })
  note.value = ''
}

function occurrence() {
  emit('log', { item_id: itemId.value, mode: 'append' })
}

function optionPart(r: ValueRow): { option_id?: number } {
  return r.optionId == null ? {} : { option_id: r.optionId }
}

function bump(r: ValueRow, sign: 1 | -1) {
  const amount = sign === 1 ? r.step : Math.min(r.step, r.current ?? 0)
  if (amount <= 0) return
  localError.value = ''
  emit('log', { item_id: itemId.value, mode: 'delta', value: sign * amount, ...optionPart(r) })
}

function setValue(r: ValueRow) {
  const n = parse(inputs[r.key])
  if (n === null || n < 0) {
    localError.value = t('today.card.err.valueMin')
    return
  }
  const delta = n - (r.current ?? 0)
  if (Math.abs(delta) < 1e-9) return
  localError.value = ''
  emit('log', { item_id: itemId.value, mode: 'delta', value: delta, ...optionPart(r) })
  inputs[r.key] = ''
}

function record(r: ValueRow) {
  const n = parse(inputs[r.key])
  if (n === null) {
    localError.value = t('today.card.err.number')
    return
  }
  if ((r.min !== null && n < r.min) || (r.max !== null && n > r.max)) {
    localError.value = t('today.card.err.range', { min: r.min ?? '-∞', max: r.max ?? '∞' })
    return
  }
  localError.value = ''
  emit('log', { item_id: itemId.value, mode: 'append', value: n, ...optionPart(r) })
  inputs[r.key] = ''
}

function addAdhoc() {
  const name = adhocName.value.trim()
  const n = parse(adhocValue.value)
  if (!name || n === null) {
    localError.value = t('today.card.err.adhoc')
    return
  }
  localError.value = ''
  emit('log', {
    item_id: itemId.value,
    mode: d.value.tracker_type === 'counter' ? 'delta' : 'append',
    value: n,
    adhoc: true,
    note: name,
  })
  adhocName.value = ''
  adhocValue.value = ''
}
</script>

<template>
  <article
    class="card card-border bg-base-100 border-base-300 shadow-xs transition-all duration-200 hover:border-primary/30"
    :class="{ 'done opacity-70 bg-base-100/60': variant === 'done' }"
    :style="d.color ? { borderLeftColor: d.color, borderLeftWidth: '4px' } : {}"
    :data-testid="`card-${d.id}`"
    :aria-busy="busy"
  >
    <div class="card-body gap-3 p-3.5 sm:p-4">
      <header class="flex flex-wrap items-center justify-between gap-2">
        <h3
          class="name text-base font-semibold flex items-center gap-2"
          :class="{ sensitive: d.effective_sensitive }"
        >
          <span
            v-if="d.color"
            class="size-2.5 rounded-full shrink-0"
            :style="{ backgroundColor: d.color }"
          />
          {{ d.name }}
        </h3>
        <span v-if="pending" class="badge badge-warning badge-sm" data-testid="pending-badge">
          <Icon icon="lucide:cloud-off" aria-hidden="true" /> {{ t('today.card.pendingSync') }}
        </span>
        <span
          v-if="category"
          class="badge badge-outline"
          :style="category.color ? { borderColor: category.color } : {}"
        >
          <Icon v-if="category.is_sensitive" icon="lucide:lock" aria-hidden="true" />
          {{ category.name }}
        </span>
      </header>

      <p
        v-if="variant === 'maintenance'"
        class="flex items-center gap-1 text-sm text-warning"
        data-testid="maintenance-text"
      >
        <Icon icon="lucide:triangle-alert" aria-hidden="true" /> {{ maintenanceText(d) }}
      </p>

      <div v-if="variant === 'quota' && quota" class="space-y-1">
        <progress
          class="progress w-full"
          :class="trafficClass"
          :value="Math.min(quota.used, quota.limit)"
          :max="quota.limit || 1"
          :aria-label="t('today.card.quotaUsed')"
        ></progress>
        <p class="text-sm" data-testid="quota-text">
          {{ quotaText(d)
          }}<span v-if="quota.exceeded" class="ml-1 font-semibold text-error">{{
            t('today.card.exceeded')
          }}</span>
        </p>
      </div>

      <!-- boolean / maintenance toggle -->
      <div v-if="isToggle" class="flex flex-wrap items-center gap-2">
        <input
          v-if="d.tracker_type === 'maintenance' && !checked"
          v-model="note"
          class="input input-sm input-bordered min-w-0 flex-1"
          :aria-label="t('today.card.notePlaceholder')"
          :placeholder="t('today.card.notePlaceholder')"
          maxlength="500"
        />
        <button
          type="button"
          class="btn"
          :class="checked ? 'btn-ghost' : 'btn-primary'"
          :disabled="busy"
          data-testid="toggle"
          :aria-pressed="checked"
          @click="toggle"
        >
          <Icon :icon="checked ? 'lucide:undo-2' : 'lucide:check'" aria-hidden="true" />
          {{
            checked
              ? t('today.card.undo')
              : d.kind === 'event'
                ? t('today.card.complete')
                : d.tracker_type === 'maintenance'
                  ? t('today.card.markDone')
                  : t('today.card.check')
          }}
        </button>
      </div>

      <!-- negative boolean: one log per occurrence -->
      <div v-if="isOccurrence" class="flex flex-wrap items-center gap-2">
        <button
          type="button"
          class="btn btn-primary"
          :disabled="busy"
          data-testid="occurrence"
          @click="occurrence"
        >
          <Icon icon="lucide:plus" aria-hidden="true" /> {{ t('today.card.logOccurrence') }}
        </button>
        <span class="text-sm" data-testid="occurrence-count">{{
          t('today.card.occurrenceCount', { count: logs.length })
        }}</span>
        <button
          v-if="logs.length"
          type="button"
          class="btn btn-ghost btn-sm"
          :disabled="busy"
          data-testid="undo-last"
          @click="emit('remove', lastLogId)"
        >
          <Icon icon="lucide:undo-2" aria-hidden="true" /> {{ t('today.card.undoLast') }}
        </button>
      </div>

      <!-- counter / metric rows -->
      <div v-for="r in rows" :key="r.key" class="space-y-2" :data-testid="`row-${r.key}`">
        <div class="flex flex-wrap items-baseline justify-between gap-2">
          <span
            v-if="r.label"
            class="name text-sm font-medium"
            :class="{ sensitive: d.effective_sensitive }"
            >{{ r.label }}</span
          >
          <span class="text-sm" data-testid="row-value">
            <template v-if="d.tracker_type === 'counter'">
              {{ fmt(r.current ?? 0)
              }}<template v-if="r.target !== null"> / {{ fmt(r.target) }}</template> {{ r.unit }}
            </template>
            <template v-else>
              {{ r.current === null ? t('today.card.notRecorded') : `${fmt(r.current)} ${r.unit}`
              }}<template v-if="r.target !== null">
                ({{ t('today.card.target', { target: fmt(r.target) }) }})</template
              >
            </template>
          </span>
        </div>
        <progress
          v-if="d.tracker_type === 'counter' && r.target"
          class="progress progress-primary w-full"
          :value="Math.min(r.current ?? 0, r.target)"
          :max="r.target"
          :aria-label="
            r.label ? t('today.card.progressFor', { label: r.label }) : t('today.card.progress')
          "
        ></progress>

        <div v-if="d.tracker_type === 'counter'" class="flex flex-wrap items-center gap-2">
          <button
            type="button"
            class="btn btn-square"
            :disabled="busy || (r.current ?? 0) <= 0"
            data-testid="dec"
            :aria-label="
              r.label
                ? t('today.card.decreaseFor', { label: r.label, step: fmt(r.step), unit: r.unit })
                : t('today.card.decrease', { step: fmt(r.step), unit: r.unit })
            "
            @click="bump(r, -1)"
          >
            <Icon icon="lucide:minus" aria-hidden="true" />
          </button>
          <button
            type="button"
            class="btn btn-primary"
            :disabled="busy"
            data-testid="inc"
            :aria-label="
              r.label
                ? t('today.card.increaseFor', { label: r.label, step: fmt(r.step), unit: r.unit })
                : t('today.card.increase', { step: fmt(r.step), unit: r.unit })
            "
            @click="bump(r, 1)"
          >
            <Icon icon="lucide:plus" aria-hidden="true" /> {{ fmt(r.step) }} {{ r.unit }}
          </button>
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

        <div v-else class="flex flex-wrap items-center gap-2">
          <input
            v-model="inputs[r.key]"
            type="number"
            inputmode="decimal"
            step="any"
            class="input input-sm input-bordered w-28"
            :aria-label="valueAria(r)"
            :placeholder="r.unit || t('today.card.value')"
            data-testid="value-input"
            @keydown.enter.prevent="record(r)"
          />
          <button
            type="button"
            class="btn btn-primary btn-sm"
            :disabled="busy"
            data-testid="record"
            @click="record(r)"
          >
            {{ t('today.card.record') }}
          </button>
        </div>
      </div>

      <!-- today's metric entries, removable -->
      <ul
        v-if="d.tracker_type === 'metric' && logs.length"
        class="flex flex-wrap gap-2"
        :aria-label="t('today.card.entries')"
      >
        <li v-for="l in logs" :key="l.id" class="badge badge-neutral gap-1">
          {{ fmt(l.value)
          }}<span v-if="l.option_id == null && hasOptions">
            {{ l.note || t('today.card.adhocLabel') }}</span
          >
          <button
            type="button"
            class="cursor-pointer"
            :disabled="busy"
            :aria-label="t('today.card.removeEntry')"
            :data-testid="`remove-${l.id}`"
            @click="emit('remove', l.id)"
          >
            <Icon icon="lucide:x" aria-hidden="true" />
          </button>
        </li>
      </ul>

      <!-- ad-hoc option -->
      <form
        v-if="hasOptions && (d.tracker_type === 'counter' || d.tracker_type === 'metric')"
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
      <ul v-if="adhoc.length && d.tracker_type === 'counter'" class="text-sm opacity-80">
        <li v-for="l in adhoc" :key="l.id">
          {{ l.note || t('today.card.adhocTitle') }}: {{ fmt(l.value) }}
        </li>
      </ul>

      <p v-if="localError" class="error text-sm" role="alert">{{ localError }}</p>
    </div>
  </article>
</template>
