<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { Icon } from '@iconify/vue'
import * as api from '../../api'
import type { CalendarDayItem, CheckinInput, CheckinLog } from '../../api'
import { canMakeUp, dayStatusText, eventTime, longDate } from '../../calendar/logic'
import { useI18n, type MessageKey } from '../../i18n'
import { useCalendarStore } from '../../stores/calendar'
import { usePrivacyStore } from '../../stores/privacy'
import MakeupControl from './MakeupControl.vue'
import ProjectBadges from '../ProjectBadges.vue'
import { useProjectsStore } from '../../stores/projects'
import OccurrenceDetailModal from '../today/OccurrenceDetailModal.vue'

const { t } = useI18n()
const store = useCalendarStore()
const privacy = usePrivacyStore()
const projectsStore = useProjectsStore()
const emit = defineEmits<{ close: [] }>()

const day = computed(() => store.selectedDay)
const date = computed(() => store.selected ?? '')
const today = computed(() => store.view?.today ?? '')
const editable = computed(() => canMakeUp(date.value, today.value))
const isFuture = computed(() => !!today.value && !!date.value && date.value > today.value)
const external = computed(() => store.externalByDate.get(date.value) ?? [])

// Items added by hand for a retroactive entry although they were not scheduled.
const added = ref<number[]>([])
const quickQuery = ref('')
const quickOpen = ref(false)
const quickBusy = ref(false)
const modalItemId = ref<number | null>(null)
const modalDetail = computed(() =>
  modalItemId.value ? (store.detailById.get(modalItemId.value) ?? null) : null,
)
const modalLogs = computed(() => (modalItemId.value ? logsOf(modalItemId.value) : []))

watch(date, () => {
  added.value = []
  quickQuery.value = ''
  quickOpen.value = false
  modalItemId.value = null
})

const listed = computed(() => (day.value?.items ?? []).filter((i) => !i.forecast || i.overdue))
const forecast = computed(() => (day.value?.items ?? []).filter((i) => i.forecast && !i.overdue))
const shownIds = computed(() => new Set([...listed.value.map((i) => i.item_id), ...added.value]))
const addable = computed(() => store.items.filter((d) => !shownIds.value.has(d.id)))
const matchingAddable = computed(() => {
  const q = quickQuery.value.trim().toLowerCase()
  if (!q) return addable.value
  return addable.value.filter((d) => d.name.toLowerCase().includes(q))
})
const addedDetails = computed(() => added.value.flatMap((id) => store.detailById.get(id) ?? []))

function onBlur() {
  setTimeout(() => {
    quickOpen.value = false
  }, 200)
}

async function submitQuick(target: { id?: number; name: string }) {
  if (quickBusy.value) return
  const targetDate = date.value
  if (!targetDate) return
  const name = target.name.trim()
  if (!name && !target.id) return
  quickBusy.value = true
  try {
    if (target.id) {
      await makeUp({ item_id: target.id, mode: 'set', value: 1 })
    } else {
      await api.createScheduledEvent(name, targetDate)
      await Promise.all([store.refresh(false), store.loadPanel()])
    }
    quickQuery.value = ''
    quickOpen.value = false
  } catch (e) {
    store.panelError = api.describeError(e)
  } finally {
    quickBusy.value = false
  }
}

const STATUS_BADGE = {
  done: { text: 'calendar.badge.done', cls: 'badge-success' },
  partial: { text: 'calendar.badge.partial', cls: 'badge-warning' },
  missed: { text: 'calendar.badge.missed', cls: 'badge-neutral' },
  frozen: { text: 'calendar.badge.frozen', cls: 'badge-info' },
  pending: { text: 'calendar.badge.pending', cls: 'badge-primary' },
  upcoming: { text: 'calendar.badge.upcoming', cls: 'badge-outline' },
  extra: { text: 'calendar.badge.extra', cls: 'badge-ghost' },
} as const
function badge(i: CalendarDayItem) {
  const b = i.overdue
    ? { text: 'calendar.badge.overdue', cls: 'badge-error' }
    : ((STATUS_BADGE as Record<string, { text: string; cls: string }>)[i.status] ??
      STATUS_BADGE.extra)
  return { text: t(b.text as MessageKey), cls: b.cls }
}
const logsOf = (id: number) => store.dayLogs.filter((l) => l.item_id === id)
const activeNoteFor = (logs: CheckinLog[]) =>
  [...logs]
    .reverse()
    .find((l) => l.note && l.note.trim())
    ?.note?.trim() ?? ''
onMounted(() => {
  void projectsStore.fetchProjects()
})

/** One-shot items (boolean / maintenance) are checked from a round button in the row header. */
function isToggleItem(d: { tracker_type: string; polarity: string } | undefined) {
  if (!d) return false
  return (
    (d.tracker_type === 'boolean' && d.polarity !== 'negative') || d.tracker_type === 'maintenance'
  )
}
const isChecked = (id: number) => logsOf(id).length > 0
function toggleItem(id: number) {
  void makeUp({ item_id: id, mode: isChecked(id) ? 'unset' : 'set' })
}
function projectOf(id: number) {
  const pid = store.detailById.get(id)?.project_id
  return pid ? (projectsStore.projectById.get(pid) ?? null) : null
}
const makeUp = (input: Omit<CheckinInput, 'date'>) => store.makeUp(input)
</script>

<template>
  <aside
    v-if="store.selected"
    class="space-y-4 card card-border bg-base-100 border-base-300 p-4 shadow-sm h-fit"
    aria-labelledby="day-panel-title"
    data-testid="day-panel"
  >
    <header class="flex items-start justify-between gap-2">
      <div>
        <h2 id="day-panel-title" class="text-lg font-semibold" tabindex="-1">
          {{ longDate(date) }}
        </h2>
        <p class="flex flex-wrap items-center gap-1 text-sm opacity-80">
          <span v-if="day?.day_type === 'holiday'" class="badge badge-success badge-sm">
            {{
              day.holiday_name
                ? t('calendar.panel.offNamed', { name: day.holiday_name })
                : t('calendar.panel.off')
            }}
          </span>
          <span v-else-if="day?.day_type === 'workday'" class="badge badge-error badge-sm">
            {{
              day.holiday_name
                ? t('calendar.panel.workdayNamed', { name: day.holiday_name })
                : t('calendar.panel.workday')
            }}
          </span>
          <span v-if="day">{{ dayStatusText(day.status) }}</span>
          <span v-if="day?.frozen" class="badge badge-info badge-sm">{{
            t('calendar.panel.frozen')
          }}</span>
        </p>
      </div>
      <button
        type="button"
        class="btn btn-ghost btn-sm btn-square"
        :aria-label="t('calendar.panel.close')"
        data-testid="panel-close"
        @click="emit('close')"
      >
        <Icon icon="lucide:x" aria-hidden="true" />
      </button>
    </header>

    <p
      v-if="store.panelError"
      class="alert alert-error text-sm"
      role="alert"
      data-testid="panel-error"
    >
      {{ store.panelError }}
    </p>

    <section v-if="external.length" aria-labelledby="panel-ext">
      <h3 id="panel-ext" class="text-sm font-semibold">{{ t('calendar.panel.external') }}</h3>
      <ul class="text-sm" data-testid="panel-external">
        <li v-for="(x, idx) in external" :key="idx" class="flex gap-2">
          <span class="w-12 shrink-0 opacity-70">{{ eventTime(x.event) }}</span>
          <span class="min-w-0 truncate"
            >{{ x.event.title }} <span class="opacity-60">({{ x.source }})</span></span
          >
        </li>
      </ul>
    </section>

    <section aria-labelledby="panel-items">
      <h3 id="panel-items" class="text-sm font-semibold">
        {{ editable ? t('calendar.panel.items') : t('calendar.panel.planned') }}
      </h3>
      <p
        v-if="!listed.length && !forecast.length && !addedDetails.length"
        class="text-sm opacity-70"
        data-testid="panel-empty"
      >
        {{ t('calendar.panel.empty') }}
      </p>
      <ul class="space-y-2">
        <li
          v-for="i in listed"
          :key="i.item_id"
          class="rounded-box border border-base-300 p-2"
          :data-testid="`panel-item-${i.item_id}`"
        >
          <div class="flex flex-wrap items-center justify-between gap-2">
            <span class="flex min-w-0 items-center gap-2">
              <button
                v-if="isToggleItem(store.detailById.get(i.item_id))"
                type="button"
                class="btn btn-circle btn-sm shrink-0"
                :class="isChecked(i.item_id) ? 'btn-success' : 'btn-outline btn-primary'"
                :disabled="store.busy.includes(i.item_id)"
                :aria-pressed="isChecked(i.item_id)"
                :aria-label="
                  isChecked(i.item_id) ? t('calendar.makeup.undo') : t('calendar.makeup.markDone')
                "
                :title="
                  isChecked(i.item_id) ? t('calendar.makeup.undo') : t('calendar.makeup.markDone')
                "
                :data-testid="`makeup-toggle-${i.item_id}`"
                @click="toggleItem(i.item_id)"
              >
                <Icon
                  :icon="isChecked(i.item_id) ? 'lucide:check' : 'lucide:circle'"
                  class="size-4"
                  aria-hidden="true"
                />
              </button>
              <span class="name font-medium" :class="{ sensitive: i.is_sensitive }">{{
                i.name
              }}</span>
            </span>
            <span class="flex flex-wrap items-center gap-1">
              <ProjectBadges :item-id="i.item_id" :project="projectOf(i.item_id)" />
              <span v-if="i.has_supplement" class="badge badge-ghost badge-sm">{{
                t('calendar.panel.makeup')
              }}</span>
              <span class="badge badge-sm" :class="badge(i).cls">{{ badge(i).text }}</span>
              <button
                type="button"
                class="btn btn-ghost btn-xs btn-square"
                :aria-label="t('today.card.details')"
                :title="t('today.card.details')"
                :data-testid="`panel-item-details-${i.item_id}`"
                @click="modalItemId = i.item_id"
              >
                <Icon icon="lucide:pencil" class="size-3.5" aria-hidden="true" />
              </button>
            </span>
          </div>
          <div
            v-if="logsOf(i.item_id).some((l) => l.note && l.note.trim())"
            class="mt-1 flex items-center gap-1 text-xs text-base-content/70"
            :data-testid="`panel-note-${i.item_id}`"
          >
            <Icon icon="lucide:pencil" class="size-3 shrink-0 opacity-70" aria-hidden="true" />
            <span class="truncate">{{ activeNoteFor(logsOf(i.item_id)) }}</span>
          </div>
          <div
            v-if="
              editable &&
              !(i.forecast && !i.overdue) &&
              !isToggleItem(store.detailById.get(i.item_id))
            "
            class="mt-2"
          >
            <MakeupControl
              v-if="store.detailById.get(i.item_id)"
              :detail="store.detailById.get(i.item_id)!"
              :logs="logsOf(i.item_id)"
              :busy="store.busy.includes(i.item_id)"
              :date="date"
              @log="makeUp"
              @remove="store.removeLog(i.item_id, $event)"
            />
            <p v-else class="text-sm opacity-60">{{ t('calendar.panel.loadingControls') }}</p>
          </div>
        </li>
        <li
          v-for="d in addedDetails"
          :key="`add-${d.id}`"
          class="rounded-box border border-dashed border-base-300 p-2"
          :data-testid="`panel-added-${d.id}`"
        >
          <div class="flex flex-wrap items-center justify-between gap-2">
            <span class="flex min-w-0 items-center gap-2">
              <button
                v-if="isToggleItem(d)"
                type="button"
                class="btn btn-circle btn-sm shrink-0"
                :class="isChecked(d.id) ? 'btn-success' : 'btn-outline btn-primary'"
                :disabled="store.busy.includes(d.id)"
                :aria-pressed="isChecked(d.id)"
                :aria-label="
                  isChecked(d.id) ? t('calendar.makeup.undo') : t('calendar.makeup.markDone')
                "
                :title="isChecked(d.id) ? t('calendar.makeup.undo') : t('calendar.makeup.markDone')"
                :data-testid="`makeup-toggle-${d.id}`"
                @click="toggleItem(d.id)"
              >
                <Icon
                  :icon="isChecked(d.id) ? 'lucide:check' : 'lucide:circle'"
                  class="size-4"
                  aria-hidden="true"
                />
              </button>
              <span class="name font-medium" :class="{ sensitive: d.effective_sensitive }">{{
                d.name
              }}</span>
            </span>
            <ProjectBadges :item-id="d.id" :project="projectOf(d.id)" />
            <button
              type="button"
              class="btn btn-ghost btn-xs btn-square"
              :aria-label="t('today.card.details')"
              :title="t('today.card.details')"
              :data-testid="`panel-item-details-${d.id}`"
              @click="modalItemId = d.id"
            >
              <Icon icon="lucide:pencil" class="size-3.5" aria-hidden="true" />
            </button>
          </div>
          <div
            v-if="logsOf(d.id).some((l) => l.note && l.note.trim())"
            class="mt-1 flex items-center gap-1 text-xs text-base-content/70"
            :data-testid="`panel-note-${d.id}`"
          >
            <Icon icon="lucide:pencil" class="size-3 shrink-0 opacity-70" aria-hidden="true" />
            <span class="truncate">{{ activeNoteFor(logsOf(d.id)) }}</span>
          </div>
          <div v-if="!isToggleItem(d)" class="mt-2">
            <MakeupControl
              :detail="d"
              :logs="logsOf(d.id)"
              :busy="store.busy.includes(d.id)"
              :date="date"
              @log="makeUp"
              @remove="store.removeLog(d.id, $event)"
            />
          </div>
        </li>
      </ul>

      <!-- Quick add / log for this day (same combobox experience as today view) -->
      <div
        class="mt-4 pt-3 border-t border-base-300/60 space-y-2"
        data-testid="panel-quick-section"
      >
        <h4 class="text-xs font-semibold text-base-content/80 flex items-center gap-1.5">
          <Icon icon="lucide:pencil" class="size-3.5 text-primary" />
          {{ t('calendar.panel.addEventTitle') }}
        </h4>
        <div class="relative w-full">
          <div class="join w-full">
            <div class="relative flex-1">
              <input
                v-model="quickQuery"
                type="text"
                class="input input-sm input-bordered w-full join-item pr-8"
                :placeholder="
                  isFuture
                    ? t('today.quickAddFuturePlaceholder')
                    : t('calendar.panel.quickPlaceholder')
                "
                autocomplete="off"
                data-testid="panel-quick-input"
                @focus="quickOpen = true"
                @blur="onBlur"
                @keydown.escape="quickOpen = false"
                @keydown.enter.prevent="submitQuick({ name: quickQuery })"
              />
              <button
                v-if="quickQuery"
                type="button"
                class="absolute right-2 top-1.5 text-base-content/40 hover:text-base-content/80"
                @click="quickQuery = ''"
              >
                <Icon icon="lucide:x" class="size-4" />
              </button>
            </div>
            <button
              type="button"
              class="btn btn-sm btn-primary join-item shadow-xs"
              :disabled="!quickQuery.trim() || quickBusy"
              data-testid="panel-quick-submit"
              @click="submitQuick({ name: quickQuery })"
            >
              <span v-if="quickBusy" class="loading loading-spinner loading-xs" />
              <Icon v-else icon="lucide:plus" class="size-4" />
              {{ t('today.addFutureEvent') }}
            </button>
          </div>

          <!-- Dropdown suggestions -->
          <div
            v-if="quickOpen && ((!isFuture && matchingAddable.length > 0) || quickQuery.trim())"
            class="absolute left-0 right-0 z-30 mt-1 max-h-60 overflow-y-auto rounded-lg border border-base-300 bg-base-100 p-1 shadow-lg"
          >
            <!-- Create new option -->
            <button
              v-if="quickQuery.trim()"
              type="button"
              class="w-full text-left px-3 py-2 text-sm rounded-md hover:bg-primary/10 text-primary font-medium flex items-center gap-2 cursor-pointer"
              @mousedown.prevent="submitQuick({ name: quickQuery })"
            >
              <Icon icon="lucide:sparkles" class="size-4 shrink-0" />
              <span>{{ t('today.createFutureEvent', { name: quickQuery.trim() }) }}</span>
            </button>

            <!-- Matching items (for today/past dates) -->
            <template v-if="!isFuture">
              <button
                v-for="u in matchingAddable"
                :key="u.id"
                type="button"
                class="w-full text-left px-3 py-2 text-sm rounded-md hover:bg-base-200 flex items-center justify-between cursor-pointer"
                @mousedown.prevent="submitQuick(u)"
              >
                <span class="font-medium text-base-content/90">{{ u.name }}</span>
                <span class="text-xs text-base-content/50">{{ t('today.card.check') }}</span>
              </button>
            </template>
          </div>
        </div>
      </div>
    </section>

    <section v-if="forecast.length" aria-labelledby="panel-forecast">
      <h3 id="panel-forecast" class="text-sm font-semibold">{{ t('calendar.panel.forecast') }}</h3>
      <ul class="text-sm" data-testid="panel-forecast">
        <li v-for="i in forecast" :key="i.item_id" class="flex items-center justify-between gap-2">
          <span class="name" :class="{ sensitive: i.is_sensitive }">{{ i.name }}</span>
          <span class="badge badge-outline badge-sm">{{ t('calendar.panel.expected') }}</span>
        </li>
      </ul>
    </section>

    <OccurrenceDetailModal
      :open="!!modalDetail"
      :detail="modalDetail"
      :logs="modalLogs"
      :date="date"
      :busy="modalDetail ? store.busy.includes(modalDetail.id) : false"
      :redact="privacy.redact"
      @close="modalItemId = null"
      @log="makeUp"
      @remove="modalDetail && store.removeLog(modalDetail.id, $event)"
      @patch-note="(id, note) => store.updateNote(id, note)"
      @set-due-date="(itemId, date) => store.setDueDate(itemId, date)"
    />
  </aside>
</template>
