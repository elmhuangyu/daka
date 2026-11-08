<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { Icon } from '@iconify/vue'
import type { Category, CheckinInput } from '../../api'
import { adhocLogs, fmt, maintenanceText, quotaText, valueRows, type CardVM, type ValueRow } from '../../today/logic'

const props = defineProps<{
  vm: CardVM
  category: Category | null
  variant: 'todo' | 'done' | 'maintenance' | 'quota'
  busy: boolean
}>()
const emit = defineEmits<{ log: [CheckinInput]; remove: [number] }>()

const d = computed(() => props.vm.detail)
const logs = computed(() => props.vm.logs)
const negative = computed(() => d.value.polarity === 'negative')
const rows = computed(() => valueRows(d.value, logs.value))
const adhoc = computed(() => adhocLogs(d.value, logs.value))
const hasOptions = computed(() => d.value.options.length > 0)
const isToggle = computed(
  () => (d.value.tracker_type === 'boolean' && !negative.value) || d.value.tracker_type === 'maintenance',
)
const isOccurrence = computed(() => d.value.tracker_type === 'boolean' && negative.value)
const checked = computed(() => logs.value.length > 0)
const itemId = computed(() => d.value.id)
const lastLogId = computed(() => logs.value.reduce((m, l) => Math.max(m, l.id), 0))

const quota = computed(() => d.value.stats?.quota ?? null)
const trafficClass = computed(() => {
  switch (quota.value?.traffic) {
    case 'red': return 'progress-error'
    case 'yellow': return 'progress-warning'
    case 'green': return 'progress-success'
    default: return 'progress-primary'
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
    localError.value = 'Enter a value of 0 or more'
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
    localError.value = 'Enter a number'
    return
  }
  if ((r.min !== null && n < r.min) || (r.max !== null && n > r.max)) {
    localError.value = `Value must be between ${r.min ?? '-inf'} and ${r.max ?? 'inf'}`
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
    localError.value = 'Ad-hoc entries need a name and a number'
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
    class="card card-border bg-base-100 border-base-300"
    :class="{ done: variant === 'done' }"
    :data-testid="`card-${d.id}`"
    :aria-busy="busy"
  >
    <div class="card-body gap-3 p-3 sm:p-4">
      <header class="flex flex-wrap items-center justify-between gap-2">
        <h3 class="name text-base font-semibold" :class="{ sensitive: d.effective_sensitive }">{{ d.name }}</h3>
        <span v-if="category" class="badge badge-outline" :style="category.color ? { borderColor: category.color } : {}">
          <Icon v-if="category.is_sensitive" icon="lucide:lock" aria-hidden="true" />
          {{ category.name }}
        </span>
      </header>

      <p v-if="variant === 'maintenance'" class="flex items-center gap-1 text-sm text-warning" data-testid="maintenance-text">
        <Icon icon="lucide:triangle-alert" aria-hidden="true" /> {{ maintenanceText(d) }}
      </p>

      <div v-if="variant === 'quota' && quota" class="space-y-1">
        <progress
          class="progress w-full" :class="trafficClass" :value="Math.min(quota.used, quota.limit)" :max="quota.limit || 1"
          :aria-label="`Quota used`"
        ></progress>
        <p class="text-sm" data-testid="quota-text">
          {{ quotaText(d) }}<span v-if="quota.exceeded" class="ml-1 font-semibold text-error">exceeded</span>
        </p>
      </div>

      <!-- boolean / maintenance toggle -->
      <div v-if="isToggle" class="flex flex-wrap items-center gap-2">
        <input
          v-if="d.tracker_type === 'maintenance' && !checked" v-model="note" class="input input-sm input-bordered min-w-0 flex-1"
          aria-label="Note (optional)" placeholder="Note (optional)" maxlength="500"
        />
        <button
          type="button" class="btn" :class="checked ? 'btn-ghost' : 'btn-primary'" :disabled="busy" data-testid="toggle"
          :aria-pressed="checked" @click="toggle"
        >
          <Icon :icon="checked ? 'lucide:undo-2' : 'lucide:check'" aria-hidden="true" />
          {{ checked ? 'Undo' : d.tracker_type === 'maintenance' ? 'Mark done' : 'Check' }}
        </button>
      </div>

      <!-- negative boolean: one log per occurrence -->
      <div v-if="isOccurrence" class="flex flex-wrap items-center gap-2">
        <button type="button" class="btn btn-primary" :disabled="busy" data-testid="occurrence" @click="occurrence">
          <Icon icon="lucide:plus" aria-hidden="true" /> Log occurrence
        </button>
        <span class="text-sm" data-testid="occurrence-count">{{ logs.length }} today</span>
        <button
          v-if="logs.length" type="button" class="btn btn-ghost btn-sm" :disabled="busy" data-testid="undo-last"
          @click="emit('remove', lastLogId)"
        >
          <Icon icon="lucide:undo-2" aria-hidden="true" /> Undo last
        </button>
      </div>

      <!-- counter / metric rows -->
      <div v-for="r in rows" :key="r.key" class="space-y-2" :data-testid="`row-${r.key}`">
        <div class="flex flex-wrap items-baseline justify-between gap-2">
          <span v-if="r.label" class="name text-sm font-medium" :class="{ sensitive: d.effective_sensitive }">{{ r.label }}</span>
          <span class="text-sm" data-testid="row-value">
            <template v-if="d.tracker_type === 'counter'">
              {{ fmt(r.current ?? 0) }}<template v-if="r.target !== null"> / {{ fmt(r.target) }}</template> {{ r.unit }}
            </template>
            <template v-else>
              {{ r.current === null ? 'Not recorded' : `${fmt(r.current)} ${r.unit}` }}<template v-if="r.target !== null"> (target {{ fmt(r.target) }})</template>
            </template>
          </span>
        </div>
        <progress
          v-if="d.tracker_type === 'counter' && r.target"
          class="progress progress-primary w-full" :value="Math.min(r.current ?? 0, r.target)" :max="r.target"
          :aria-label="`Progress${r.label ? ' for ' + r.label : ''}`"
        ></progress>

        <div v-if="d.tracker_type === 'counter'" class="flex flex-wrap items-center gap-2">
          <button
            type="button" class="btn btn-square" :disabled="busy || (r.current ?? 0) <= 0" data-testid="dec"
            :aria-label="`Decrease${r.label ? ' ' + r.label : ''} by ${fmt(r.step)} ${r.unit}`" @click="bump(r, -1)"
          ><Icon icon="lucide:minus" aria-hidden="true" /></button>
          <button
            type="button" class="btn btn-primary" :disabled="busy" data-testid="inc"
            :aria-label="`Increase${r.label ? ' ' + r.label : ''} by ${fmt(r.step)} ${r.unit}`" @click="bump(r, 1)"
          ><Icon icon="lucide:plus" aria-hidden="true" /> {{ fmt(r.step) }} {{ r.unit }}</button>
          <input
            v-model="inputs[r.key]" type="number" inputmode="decimal" min="0" step="any" class="input input-sm input-bordered w-24"
            :aria-label="`Set total${r.label ? ' for ' + r.label : ''}`" placeholder="Set to" data-testid="set-input"
            @keydown.enter.prevent="setValue(r)"
          />
          <button type="button" class="btn btn-sm" :disabled="busy" data-testid="set" @click="setValue(r)">Set</button>
        </div>

        <div v-else class="flex flex-wrap items-center gap-2">
          <input
            v-model="inputs[r.key]" type="number" inputmode="decimal" step="any" class="input input-sm input-bordered w-28"
            :aria-label="`Value${r.label ? ' for ' + r.label : ''}${r.unit ? ' in ' + r.unit : ''}`" :placeholder="r.unit || 'Value'"
            data-testid="value-input" @keydown.enter.prevent="record(r)"
          />
          <button type="button" class="btn btn-primary btn-sm" :disabled="busy" data-testid="record" @click="record(r)">Record</button>
        </div>
      </div>

      <!-- today's metric entries, removable -->
      <ul v-if="d.tracker_type === 'metric' && logs.length" class="flex flex-wrap gap-2" aria-label="Today's entries">
        <li v-for="l in logs" :key="l.id" class="badge badge-neutral gap-1">
          {{ fmt(l.value) }}<span v-if="l.option_id == null && hasOptions"> {{ l.note || 'ad-hoc' }}</span>
          <button type="button" class="cursor-pointer" :disabled="busy" aria-label="Remove entry" :data-testid="`remove-${l.id}`" @click="emit('remove', l.id)">
            <Icon icon="lucide:x" aria-hidden="true" />
          </button>
        </li>
      </ul>

      <!-- ad-hoc option -->
      <form
        v-if="hasOptions && (d.tracker_type === 'counter' || d.tracker_type === 'metric')"
        class="flex flex-wrap items-center gap-2" data-testid="adhoc-form" @submit.prevent="addAdhoc"
      >
        <input v-model="adhocName" class="input input-sm input-bordered min-w-0 flex-1" aria-label="Ad-hoc option name" placeholder="Other (name)" maxlength="100" />
        <input v-model="adhocValue" type="number" inputmode="decimal" step="any" class="input input-sm input-bordered w-24" aria-label="Ad-hoc value" placeholder="Value" />
        <button type="submit" class="btn btn-sm" :disabled="busy" data-testid="adhoc-add">Add</button>
      </form>
      <ul v-if="adhoc.length && d.tracker_type === 'counter'" class="text-sm opacity-80">
        <li v-for="l in adhoc" :key="l.id">{{ l.note || 'Ad-hoc' }}: {{ fmt(l.value) }}</li>
      </ul>

      <p v-if="localError" class="error text-sm" role="alert">{{ localError }}</p>
    </div>
  </article>
</template>
