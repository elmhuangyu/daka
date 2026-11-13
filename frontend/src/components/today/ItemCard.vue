<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { Icon } from '@iconify/vue'
import ProjectBadges from '../ProjectBadges.vue'
import type { Category, CheckinInput, Project } from '../../api'
import { formatCustomDate } from '../../i18n/format'
import { useI18n } from '../../i18n'
import {
  fmt,
  maintenanceText,
  quotaPace,
  quotaPaceText,
  quotaText,
  valueRows,
  type CardVM,
  type ValueRow,
} from '../../today/logic'

const { t } = useI18n()

const props = defineProps<{
  vm: CardVM
  category: Category | null
  project?: Project | null
  variant: 'todo' | 'done' | 'maintenance' | 'quota'
  busy: boolean
  /** The local date being viewed, used for quota pacing. */
  date?: string
  /** An offline check-in for this item is queued and not yet confirmed. */
  pending?: boolean
  /** Show the drag handle for manual reordering. */
  reorderable?: boolean
}>()
const emit = defineEmits<{
  log: [CheckinInput]
  remove: [number]
  openDetails: []
  dragStart: [PointerEvent]
}>()

const d = computed(() => props.vm.detail)
const logs = computed(() => props.vm.logs)
const negative = computed(() => d.value.polarity === 'negative')
const rows = computed(() => valueRows(d.value, logs.value))
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

const activeNote = computed(
  () =>
    [...logs.value]
      .reverse()
      .find((l) => l.note && l.note.trim())
      ?.note?.trim() ?? '',
)

const quota = computed(() => d.value.stats?.quota ?? null)
const pace = computed(() => quotaPace(d.value, props.date))
const paceClass = computed(() => {
  switch (pace.value?.state) {
    case 'met':
      return 'text-success'
    case 'must':
      return 'text-warning font-medium'
    case 'behind':
      return 'text-base-content/60'
    default:
      return 'text-base-content/70'
  }
})
const trafficClass = computed(() => {
  if (pace.value?.state === 'met') return 'progress-success'
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

const toggleLabel = computed(() =>
  checked.value
    ? t('today.card.undo')
    : d.value.kind === 'event'
      ? t('today.card.complete')
      : d.value.tracker_type === 'maintenance'
        ? t('today.card.markDone')
        : t('today.card.check'),
)

function toggle() {
  emit('log', {
    item_id: itemId.value,
    mode: checked.value ? 'unset' : 'set',
  })
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
</script>

<template>
  <article
    class="card card-border bg-base-100 border-base-300 shadow-xs transition-all duration-200 hover:border-primary/30 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
    :class="{ 'done opacity-70 bg-base-100/60': variant === 'done' }"
    :style="d.color ? { borderLeftColor: d.color, borderLeftWidth: '4px' } : {}"
    :data-testid="`card-${d.id}`"
    data-card
    tabindex="-1"
    :aria-busy="busy"
    @keydown.enter.self="emit('openDetails')"
  >
    <div class="card-body gap-2 p-2.5 sm:px-3.5">
      <header class="flex flex-wrap items-center justify-between gap-x-2 gap-y-1">
        <div class="flex min-w-0 items-center gap-2">
          <button
            v-if="isToggle"
            type="button"
            class="btn btn-circle btn-sm shrink-0"
            :class="checked ? 'btn-success' : 'btn-outline btn-primary'"
            :disabled="busy"
            data-testid="toggle"
            :aria-pressed="checked"
            :aria-label="toggleLabel"
            :title="toggleLabel"
            @click="toggle"
          >
            <Icon
              :icon="checked ? 'lucide:check' : 'lucide:circle'"
              class="size-4"
              aria-hidden="true"
            />
          </button>
          <h3
            class="name text-base font-semibold flex items-center gap-2 cursor-pointer hover:text-primary transition-colors"
            :class="{
              sensitive: d.effective_sensitive,
            }"
            @click="emit('openDetails')"
          >
            <span v-if="d.emoji" aria-hidden="true" class="shrink-0">{{ d.emoji }}</span>
            {{ d.name }}
          </h3>
        </div>
        <div class="flex flex-wrap items-center gap-1.5">
          <span
            v-if="d.kind === 'event' && d.current_rule.schedule_type !== 'none'"
            class="badge badge-sm"
            :class="
              d.current_rule.schedule_type === 'none'
                ? 'badge-ghost'
                : 'badge-primary badge-outline font-mono'
            "
            data-testid="todo-date-badge"
          >
            <Icon
              :icon="d.current_rule.schedule_type === 'none' ? 'lucide:inbox' : 'lucide:calendar'"
              class="size-3"
              aria-hidden="true"
            />
            {{
              d.current_rule.schedule_type === 'none'
                ? t('today.undated')
                : formatCustomDate(props.vm.today?.due_date || d.current_rule.dtstart || '')
            }}
          </span>
          <span v-if="pending" class="badge badge-warning badge-sm" data-testid="pending-badge">
            <Icon icon="lucide:cloud-off" aria-hidden="true" /> {{ t('today.card.pendingSync') }}
          </span>
          <ProjectBadges :item-id="d.id" :project="project ?? null" />
          <span
            v-if="category"
            class="badge badge-outline"
            :style="category.color ? { borderColor: category.color } : {}"
          >
            <Icon v-if="category.is_sensitive" icon="lucide:lock" aria-hidden="true" />
            {{ category.name }}
          </span>
          <button
            type="button"
            class="btn btn-ghost btn-xs btn-square"
            :aria-label="t('today.card.details')"
            :title="t('today.card.details')"
            data-testid="open-details"
            @click="emit('openDetails')"
          >
            <Icon icon="lucide:pencil" class="size-3.5" aria-hidden="true" />
          </button>
          <button
            v-if="reorderable"
            type="button"
            class="btn btn-ghost btn-xs btn-square cursor-grab touch-none select-none"
            :aria-label="t('today.card.reorder')"
            :title="t('today.card.reorder')"
            data-testid="drag-handle"
            @pointerdown="emit('dragStart', $event)"
          >
            <Icon icon="lucide:menu" class="size-3.5" aria-hidden="true" />
          </button>
        </div>
      </header>

      <div
        v-if="activeNote"
        class="flex items-center gap-1.5 text-xs text-base-content/70 bg-base-200/50 hover:bg-base-200 rounded-md px-2.5 py-1 cursor-pointer transition-colors"
        data-testid="card-note-preview"
        @click="emit('openDetails')"
      >
        <Icon icon="lucide:pencil" class="size-3 shrink-0 opacity-70" aria-hidden="true" />
        <span class="truncate" :class="{ sensitive: d.effective_sensitive }">{{ activeNote }}</span>
      </div>

      <p
        v-if="variant === 'maintenance'"
        class="flex items-center gap-1 text-sm text-warning"
        data-testid="maintenance-text"
      >
        <Icon icon="lucide:triangle-alert" aria-hidden="true" /> {{ maintenanceText(d) }}
      </p>

      <div v-if="variant === 'quota' && quota" class="space-y-1">
        <div
          v-if="pace?.pips"
          class="flex items-center gap-1.5"
          role="img"
          :aria-label="quotaText(d)"
          data-testid="quota-pips"
        >
          <span
            v-for="i in quota.limit"
            :key="i"
            class="size-3.5 rounded-full border-2 transition-colors"
            :class="
              i <= quota.used
                ? pace.state === 'met'
                  ? 'bg-success border-success'
                  : 'bg-primary border-primary'
                : pace.state === 'must'
                  ? 'border-warning'
                  : 'border-base-content/30'
            "
          ></span>
          <span
            v-if="pace.extra > 0"
            class="badge badge-success badge-sm font-semibold"
            data-testid="quota-bonus"
            >+{{ pace.extra }}</span
          >
        </div>
        <progress
          v-else
          class="progress w-full"
          :class="trafficClass"
          :value="Math.min(quota.used, quota.limit)"
          :max="quota.limit || 1"
          :aria-label="t('today.card.quotaUsed')"
        ></progress>
        <p v-if="pace" class="text-sm" :class="paceClass" data-testid="quota-pace">
          <Icon
            v-if="pace.state === 'met'"
            icon="lucide:check-circle-2"
            class="inline size-4 align-text-bottom"
            aria-hidden="true"
          />
          {{ quotaPaceText(pace) }}
        </p>
        <p
          class="text-sm"
          :class="pace ? 'text-xs text-base-content/60' : ''"
          data-testid="quota-text"
        >
          {{ quotaText(d)
          }}<span v-if="quota.exceeded" class="ml-1 font-semibold text-error">{{
            t('today.card.exceeded')
          }}</span>
        </p>
      </div>

      <!-- negative boolean: one log per occurrence -->
      <div v-if="isOccurrence" class="flex flex-wrap items-center gap-x-3 gap-y-1">
        <span class="text-sm" data-testid="occurrence-count">{{
          t('today.card.occurrenceCount', { count: logs.length })
        }}</span>
        <div class="ml-auto flex items-center gap-1.5">
          <button
            type="button"
            class="btn btn-sm"
            :disabled="busy || !logs.length"
            data-testid="undo-last"
            :aria-label="t('today.card.undoLast')"
            :title="t('today.card.undoLast')"
            @click="emit('remove', lastLogId)"
          >
            −1
          </button>
          <button
            type="button"
            class="btn btn-sm btn-primary"
            :disabled="busy"
            data-testid="occurrence"
            :aria-label="t('today.card.logOccurrence')"
            :title="t('today.card.logOccurrence')"
            @click="occurrence"
          >
            +1
          </button>
        </div>
      </div>

      <!-- counter / metric rows -->
      <div
        v-for="r in rows"
        :key="r.key"
        :class="
          d.tracker_type === 'counter' ? 'flex flex-wrap items-center gap-x-3 gap-y-1' : 'space-y-2'
        "
        :data-testid="`row-${r.key}`"
      >
        <div
          :class="
            d.tracker_type === 'counter'
              ? 'contents'
              : 'flex flex-wrap items-baseline justify-between gap-2'
          "
        >
          <span
            v-if="r.label"
            class="name text-sm font-medium"
            :class="{ sensitive: d.effective_sensitive }"
            >{{ r.label }}</span
          >
          <span class="text-sm flex items-center gap-1.5" data-testid="row-value">
            <template v-if="d.tracker_type === 'counter'">
              <span>
                {{ fmt(r.current ?? 0)
                }}<template v-if="r.target !== null"> / {{ fmt(r.target) }}</template> {{ r.unit }}
              </span>
              <span
                v-if="r.target !== null && (r.current ?? 0) >= r.target"
                class="badge badge-success badge-xs gap-1 font-normal"
                data-testid="target-met-badge"
              >
                <Icon icon="lucide:check" class="size-3" />
                {{ t('today.card.goalMet') }}
              </span>
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
          class="progress min-w-16 flex-1"
          :class="(r.current ?? 0) >= r.target ? 'progress-success' : 'progress-primary'"
          :value="Math.min(r.current ?? 0, r.target)"
          :max="r.target"
          :aria-label="
            r.label ? t('today.card.progressFor', { label: r.label }) : t('today.card.progress')
          "
        ></progress>

        <div v-if="d.tracker_type === 'counter'" class="ml-auto flex items-center gap-1.5">
          <button
            type="button"
            class="btn btn-sm"
            :disabled="busy || (r.current ?? 0) <= 0"
            data-testid="dec"
            :aria-label="
              r.label
                ? t('today.card.decreaseFor', { label: r.label, step: fmt(r.step), unit: r.unit })
                : t('today.card.decrease', { step: fmt(r.step), unit: r.unit })
            "
            @click="bump(r, -1)"
          >
            −{{ fmt(r.step) }}
          </button>
          <button
            type="button"
            class="btn btn-sm"
            :class="
              r.target !== null && (r.current ?? 0) >= r.target
                ? 'btn-outline btn-success'
                : 'btn-primary'
            "
            :disabled="busy"
            data-testid="inc"
            :aria-label="
              r.label
                ? t('today.card.increaseFor', { label: r.label, step: fmt(r.step), unit: r.unit })
                : t('today.card.increase', { step: fmt(r.step), unit: r.unit })
            "
            @click="bump(r, 1)"
          >
            +{{ fmt(r.step) }}
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

      <p v-if="localError" class="error text-sm" role="alert">{{ localError }}</p>
    </div>
  </article>
</template>
