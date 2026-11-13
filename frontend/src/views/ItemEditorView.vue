<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { Icon } from '@iconify/vue'
import EmojiPicker from '../components/EmojiPicker.vue'
import { useRoute, useRouter } from 'vue-router'
import {
  createCategory,
  deleteItem,
  deleteOption,
  getItem,
  listCategories,
  listProjects,
  patchItem,
  postItem,
  postOption,
  putOption,
  type CategoryDto,
  type ItemDto,
  type Project,
} from '../api'
import {
  allowedMeasures,
  allowedPolarities,
  allowedSchedules,
  buildCreateBody,
  buildEditPlan,
  describeItemError,
  emptyForm,
  formFromItem,
  holidayAllowed,
  isNumeric,
  normalizeForm,
  optionRequest,
  previewRule,
  quotaNeedsOption,
  validateForm,
  type ItemForm,
} from '../itemForm'
import { localIso, WEEKDAYS } from '../rrule'
import { useItemsStore } from '../stores/items'
import { usePrivacyStore } from '../stores/privacy'
import { useI18n } from '../i18n'
import { formatCustomDate, monthName, weekdayName } from '../i18n/format'
import DateInput from '../components/DateInput.vue'

const { t, tParts } = useI18n()

const route = useRoute()
const router = useRouter()
const privacy = usePrivacyStore()
const panel = useItemsStore()

const editId = computed(() => (route.params.id ? Number(route.params.id) : null))
const isEdit = computed(() => editId.value !== null)

const form = reactive<ItemForm>(emptyForm())
const original = ref<ItemDto | null>(null)
const categories = ref<CategoryDto[]>([])
const projects = ref<Project[]>([])
const loading = ref(false)
const busy = ref(false)
const loadError = ref('')
const submitError = ref('')
const showErrors = ref(false)
const retro = ref(false)
const effectiveFrom = ref('')
const newCategory = ref('')
const newCategorySensitive = ref(false)
const showAddCategory = ref(false)

const COLOR_PRESETS = [
  { label: 'None', value: '' },
  { label: 'Emerald', value: '#10b981' },
  { label: 'Blue', value: '#3b82f6' },
  { label: 'Indigo', value: '#6366f1' },
  { label: 'Purple', value: '#8b5cf6' },
  { label: 'Pink', value: '#ec4899' },
  { label: 'Rose', value: '#f43f5e' },
  { label: 'Amber', value: '#f59e0b' },
  { label: 'Orange', value: '#f97316' },
  { label: 'Teal', value: '#14b8a6' },
  { label: 'Cyan', value: '#06b6d4' },
]
const today = localIso()

const TRACKERS = ['boolean', 'counter', 'metric', 'maintenance'] as const
// 0 = Sunday .. 6 = Saturday for the short weekday names.
const WD_INDEX: Record<string, number> = { MO: 1, TU: 2, WE: 3, TH: 4, FR: 5, SA: 6, SU: 0 }

// Editing an existing item needs plaintext; with the privacy guard on the
// server redacts names, so saving would overwrite them. Block instead.
const blocked = computed(() => isEdit.value && privacy.redact)

async function load() {
  loadError.value = ''
  if (blocked.value) return
  loading.value = true
  try {
    const [cats, projs, item] = await Promise.all([
      privacy.redact ? Promise.resolve([]) : listCategories(),
      listProjects({ status: 'active', redact: privacy.redact }),
      isEdit.value ? getItem(editId.value!) : Promise.resolve(null),
    ])
    categories.value = cats
    projects.value = projs
    if (item) {
      if (item.kind === 'event') {
        await router.replace('/')
        return
      }
      original.value = item
      Object.assign(form, formFromItem(item))
      form.projectId = item.project_id ?? null
    } else if (!form.dtstart) {
      form.dtstart = today
    }
  } catch (e) {
    loadError.value = describeItemError(e)
  } finally {
    loading.value = false
  }
}
onMounted(load)
watch(
  () => privacy.redact,
  (on) => {
    if (on) {
      // Drop plaintext held by the editor as soon as the guard turns on.
      categories.value = []
      projects.value = []
      original.value = null
      Object.assign(form, emptyForm())
    } else {
      void load()
    }
  },
)

watch(
  () => [
    form.trackerType,
    form.polarity,
    form.scheduleType,
    form.quotaMeasure,
    form.options.length,
  ],
  () => normalizeForm(form),
)

const errors = computed(() => validateForm(form))
const preview = computed(() => {
  const o = original.value
  const phase =
    form.dtstart ||
    (o && o.current_rule.schedule_type === 'fixed_rrule' && o.current_rule.dtstart) ||
    today
  return previewRule(form, today, phase)
})
const ruleWillChange = computed(() => {
  if (!original.value) return false
  const p = buildEditPlan(original.value, form, retro.value ? effectiveFrom.value : '', today)
  return p.ruleChanged
})

function addOption() {
  form.options.push({ name: '', unit: '', step: '', target: '' })
}
function removeOption(i: number) {
  form.options.splice(i, 1)
  if (form.quotaOption !== null)
    form.quotaOption =
      form.quotaOption === i ? null : form.quotaOption > i ? form.quotaOption - 1 : form.quotaOption
}
function toggleDay(d: (typeof WEEKDAYS)[number]) {
  const i = form.weekDays.indexOf(d)
  if (i >= 0) form.weekDays.splice(i, 1)
  else form.weekDays.push(d)
}

async function addCategory() {
  const name = newCategory.value.trim()
  if (!name) return
  try {
    const c = await createCategory(name, newCategorySensitive.value)
    categories.value.push(c)
    form.categoryId = c.id
    newCategory.value = ''
    newCategorySensitive.value = false
    showAddCategory.value = false
  } catch (e) {
    submitError.value = describeItemError(e)
  }
}

const confirmDelete = ref(false)

async function removeItem() {
  if (busy.value || editId.value === null) return
  busy.value = true
  submitError.value = ''
  try {
    await deleteItem(editId.value)
    void panel.refresh()
    await router.push('/items')
  } catch (e) {
    submitError.value = describeItemError(e)
    confirmDelete.value = false
  } finally {
    busy.value = false
  }
}

async function submit() {
  if (busy.value) return
  showErrors.value = true
  submitError.value = ''
  if (errors.value.length > 0) return
  busy.value = true
  try {
    if (isEdit.value) await saveEdit()
    else await saveCreate()
  } catch (e) {
    submitError.value = describeItemError(e)
  } finally {
    busy.value = false
  }
}

async function saveCreate() {
  const body = buildCreateBody(form)
  body.project_id = form.projectId
  const item = await postItem(body)
  if (form.remindTime !== '') {
    try {
      await patchItem(item.id, { remind_time: form.remindTime })
    } catch (e) {
      // The item exists; continue in edit mode so the reminder can be retried.
      await router.replace({ name: 'item-edit', params: { id: item.id } })
      submitError.value = t('items.editor.reminderNotSaved', { reason: describeItemError(e) })
      return
    }
  }
  await router.push('/')
}

async function saveEdit() {
  const orig = original.value!
  const plan = buildEditPlan(orig, form, retro.value ? effectiveFrom.value : '', today)
  const optionIds = new Map<number, number>() // form index -> server id
  form.options.forEach((o, i) => {
    if (o.id !== undefined) optionIds.set(i, o.id)
  })
  for (const i of plan.addOptions) {
    const created = await postOption(orig.id, optionRequest(form.options[i], i))
    optionIds.set(i, created.id)
    form.options[i].id = created.id
  }
  for (const i of plan.updateOptions)
    await putOption(orig.id, form.options[i].id!, optionRequest(form.options[i], i))
  const rule = plan.patch.rule as Record<string, unknown> | undefined
  if (rule && form.quotaOption !== null && quotaNeedsOption(form))
    rule.quota_option_id = optionIds.get(form.quotaOption)
  if (form.projectId !== (orig.project_id ?? null)) {
    plan.patch.project_id = form.projectId
    plan.hasPatch = true
  }
  if (plan.hasPatch) await patchItem(orig.id, plan.patch)
  for (const id of plan.removeOptionIds) await deleteOption(orig.id, id)
  await router.push('/')
}

const showHoliday = computed(() => holidayAllowed(form))
</script>

<template>
  <section class="mx-auto max-w-2xl p-4 space-y-4" data-testid="item-editor">
    <header class="pb-1">
      <h1 class="text-2xl font-bold tracking-tight">
        {{ isEdit ? t('items.editor.titleEdit') : t('items.editor.titleNew') }}
      </h1>
    </header>

    <p v-if="blocked" class="alert alert-warning" role="alert" data-testid="privacy-blocked">
      {{ t('items.editor.blocked') }}
    </p>
    <p v-else-if="loadError" class="alert alert-error" role="alert">{{ loadError }}</p>
    <p v-else-if="loading" class="py-8 text-center opacity-70">
      <span class="loading loading-spinner loading-sm" aria-hidden="true"></span>
      {{ t('common.loading') }}
    </p>

    <form v-else class="flex flex-col gap-5" novalidate @submit.prevent="submit">
      <!-- Section 1: Basic Details -->
      <div class="card card-border bg-base-100 border-base-300 p-5 shadow-xs space-y-4">
        <h2
          class="text-xs font-semibold uppercase tracking-wider text-base-content/70 flex items-center gap-2"
        >
          <Icon icon="lucide:pencil" class="size-4 text-primary" aria-hidden="true" />
          {{ t('items.editor.section.basic') }}
        </h2>

        <!-- Habit Name -->
        <label class="flex flex-col gap-1.5">
          <span class="text-sm font-medium">{{ t('items.editor.name') }}</span>
          <input
            v-model="form.name"
            class="input input-bordered w-full text-base font-medium"
            maxlength="200"
            autocomplete="off"
            :placeholder="t('items.editor.namePlaceholderHabit')"
            data-testid="f-name"
          />
        </label>

        <!-- Category -->
        <fieldset v-if="!privacy.redact" class="flex flex-col gap-2">
          <div class="flex items-center justify-between">
            <legend class="text-sm font-medium">{{ t('items.editor.category') }}</legend>
            <button
              type="button"
              class="btn btn-xs btn-ghost gap-1 text-xs opacity-75 hover:opacity-100"
              @click="showAddCategory = !showAddCategory"
            >
              <Icon :icon="showAddCategory ? 'lucide:minus' : 'lucide:plus'" class="size-3" />
              {{ t('items.editor.newCategoryAction') }}
            </button>
          </div>
          <select v-model="form.categoryId" class="select select-bordered" data-testid="f-category">
            <option :value="null">{{ t('items.editor.noCategory') }}</option>
            <option v-for="c in categories" :key="c.id" :value="c.id">
              {{ c.is_sensitive ? t('items.editor.catSensitive', { name: c.name }) : c.name }}
            </option>
          </select>
          <div v-show="showAddCategory" class="flex flex-wrap items-center gap-2 pt-1">
            <input
              v-model="newCategory"
              class="input input-bordered input-sm flex-1 min-w-36"
              :placeholder="t('items.editor.newCategory')"
              maxlength="200"
              data-testid="f-new-category"
            />
            <label class="flex items-center gap-1.5 text-xs cursor-pointer select-none">
              <input v-model="newCategorySensitive" type="checkbox" class="checkbox checkbox-xs" />
              {{ t('items.editor.sensitive') }}
            </label>
            <button
              type="button"
              class="btn btn-sm btn-primary"
              data-testid="add-category"
              @click="addCategory"
            >
              {{ t('common.add') }}
            </button>
          </div>
        </fieldset>

        <!-- Project -->
        <fieldset v-if="!privacy.redact" class="flex flex-col gap-2">
          <legend class="text-sm font-medium">{{ t('projects.project') }}</legend>
          <select v-model="form.projectId" class="select select-bordered" data-testid="f-project">
            <option :value="null">{{ t('projects.noProject') }}</option>
            <option v-for="p in projects" :key="p.id" :value="p.id">{{ p.name }}</option>
          </select>
        </fieldset>

        <div class="flex flex-col gap-1.5">
          <span class="text-sm font-medium">{{ t('emoji.label') }}</span>
          <EmojiPicker v-model="form.emoji" testid="f-emoji" />
        </div>

        <!-- Color and Privacy Toggle -->
        <div class="flex flex-wrap items-center justify-between gap-4 pt-1">
          <div class="flex flex-col gap-1.5">
            <span class="text-sm font-medium">{{ t('items.editor.color') }}</span>
            <div class="flex flex-wrap items-center gap-2">
              <button
                v-for="p in COLOR_PRESETS"
                :key="p.value"
                type="button"
                class="size-7 rounded-full border-2 flex items-center justify-center transition-transform hover:scale-110 cursor-pointer"
                :class="
                  form.color === p.value
                    ? 'border-primary ring-2 ring-primary/30'
                    : 'border-base-300'
                "
                :style="p.value ? { backgroundColor: p.value } : {}"
                :title="p.label"
                :aria-label="p.label"
                data-testid="color-picker-item"
                @click="form.color = p.value"
              >
                <Icon v-if="!p.value" icon="lucide:ban" class="size-4 opacity-50" />
                <Icon
                  v-else-if="form.color === p.value"
                  icon="lucide:check"
                  class="size-4 text-white drop-shadow-xs"
                />
              </button>
              <label
                class="flex items-center gap-1.5 ml-2 cursor-pointer"
                :title="t('items.editor.colorCustom')"
              >
                <input
                  v-model="form.color"
                  type="color"
                  class="size-7 p-0 rounded border border-base-300 cursor-pointer bg-transparent"
                  :aria-label="t('items.editor.colorCustom')"
                />
              </label>
            </div>
          </div>

          <label
            class="flex items-center gap-2 cursor-pointer select-none text-sm font-medium self-end pb-1"
          >
            <input
              v-model="form.isSensitive"
              type="checkbox"
              class="checkbox checkbox-primary"
              data-testid="f-sensitive"
            />
            {{ t('items.editor.sensitive') }}
          </label>
        </div>
      </div>

      <!-- Section 2: Tracking & Goals -->
      <div class="card card-border bg-base-100 border-base-300 p-5 shadow-xs space-y-4">
        <h2
          class="text-xs font-semibold uppercase tracking-wider text-base-content/70 flex items-center gap-2"
        >
          <Icon icon="lucide:check-circle-2" class="size-4 text-primary" aria-hidden="true" />
          {{ t('items.editor.section.tracking') }}
        </h2>

        <fieldset class="flex flex-col gap-2.5" :disabled="isEdit">
          <legend class="text-sm font-medium">{{ t('items.editor.trackingMethod') }}</legend>
          <div class="join flex-wrap shadow-xs">
            <button
              v-for="tk in TRACKERS"
              :key="tk"
              type="button"
              class="btn join-item btn-sm"
              :class="{ 'btn-primary': form.trackerType === tk }"
              :data-testid="`tracker-${tk}`"
              @click="form.trackerType = tk"
            >
              {{ t(`items.editor.tracker.${tk}`) }}
            </button>
          </div>
          <p v-if="form.trackerType === 'counter'" class="text-xs text-base-content/60 pt-1">
            {{ t('items.editor.counterLimitHint') }}
          </p>
          <div
            v-if="allowedPolarities(form.trackerType).length > 1"
            class="flex items-center gap-4 pt-1"
          >
            <label class="flex items-center gap-1.5 cursor-pointer text-sm">
              <input
                v-model="form.polarity"
                type="radio"
                value="positive"
                class="radio radio-sm radio-primary"
              />
              {{ t('items.editor.positive') }}
            </label>
            <label class="flex items-center gap-1.5 cursor-pointer text-sm">
              <input
                v-model="form.polarity"
                type="radio"
                value="negative"
                class="radio radio-sm radio-primary"
                data-testid="polarity-negative"
              />
              {{ t('items.editor.negative') }}
            </label>
          </div>
          <p v-if="isEdit" class="text-xs opacity-60">{{ t('items.editor.immutable') }}</p>
        </fieldset>

        <fieldset v-if="isNumeric(form.trackerType)" class="grid grid-cols-2 gap-3 pt-2">
          <legend class="text-sm mb-1 font-medium">{{ t('items.editor.measurement') }}</legend>
          <label class="flex flex-col gap-1">
            <span class="text-xs text-base-content/70">{{ t('items.editor.unit') }}</span>
            <input
              v-model="form.unit"
              class="input input-bordered input-sm"
              maxlength="32"
              data-testid="f-unit"
            />
          </label>
          <label class="flex flex-col gap-1">
            <span class="text-xs text-base-content/70">{{ t('items.editor.step') }}</span>
            <input v-model="form.step" inputmode="decimal" class="input input-bordered input-sm" />
          </label>
          <label class="flex flex-col gap-1">
            <span class="text-xs text-base-content/70">{{ t('items.editor.min') }}</span>
            <input
              v-model="form.minVal"
              inputmode="decimal"
              class="input input-bordered input-sm"
            />
          </label>
          <label class="flex flex-col gap-1">
            <span class="text-xs text-base-content/70">{{ t('items.editor.max') }}</span>
            <input
              v-model="form.maxVal"
              inputmode="decimal"
              class="input input-bordered input-sm"
            />
          </label>
          <label v-if="form.trackerType === 'metric'" class="col-span-2 flex flex-col gap-1">
            <span class="text-xs text-base-content/70">{{ t('items.editor.aggregate') }}</span>
            <select v-model="form.aggregate" class="select select-bordered select-sm">
              <option value="">{{ t('items.editor.aggDefault') }}</option>
              <option value="sum">{{ t('items.editor.aggSum') }}</option>
              <option value="latest">{{ t('items.editor.aggLatest') }}</option>
              <option value="avg">{{ t('items.editor.aggAvg') }}</option>
            </select>
          </label>
          <label
            v-if="form.scheduleType !== 'quota' && form.options.length === 0"
            class="col-span-2 flex flex-col gap-1"
          >
            <span class="text-xs text-base-content/70">{{ t('items.editor.dailyTarget') }}</span>
            <input
              v-model="form.target"
              inputmode="decimal"
              class="input input-bordered input-sm"
              data-testid="f-target"
            />
          </label>
        </fieldset>

        <fieldset v-if="isNumeric(form.trackerType)" class="flex flex-col gap-3 pt-2">
          <legend class="text-sm font-medium">{{ t('items.editor.options') }}</legend>
          <div
            v-for="(o, i) in form.options"
            :key="i"
            class="flex flex-wrap items-end gap-2 rounded-lg border border-base-300 bg-base-200/40 p-2.5"
          >
            <label class="flex min-w-32 flex-1 flex-col gap-1">
              <span class="text-xs text-base-content/70">{{ t('items.editor.name') }}</span>
              <input
                v-model="o.name"
                class="input input-bordered input-sm"
                maxlength="200"
                :data-testid="`opt-name-${i}`"
              />
            </label>
            <label class="flex w-20 flex-col gap-1">
              <span class="text-xs text-base-content/70">{{ t('items.editor.unit') }}</span>
              <input v-model="o.unit" class="input input-bordered input-sm" />
            </label>
            <label class="flex w-20 flex-col gap-1">
              <span class="text-xs text-base-content/70">{{ t('items.editor.step') }}</span>
              <input v-model="o.step" inputmode="decimal" class="input input-bordered input-sm" />
            </label>
            <label class="flex w-24 flex-col gap-1">
              <span class="text-xs text-base-content/70">{{ t('items.editor.target') }}</span>
              <input v-model="o.target" inputmode="decimal" class="input input-bordered input-sm" />
            </label>
            <button
              type="button"
              class="btn btn-ghost btn-sm btn-square"
              :aria-label="t('items.editor.removeOption')"
              @click="removeOption(i)"
            >
              <Icon icon="lucide:trash-2" aria-hidden="true" />
            </button>
          </div>
          <button
            type="button"
            class="btn btn-sm btn-outline self-start"
            data-testid="add-option"
            @click="addOption"
          >
            <Icon icon="lucide:plus" aria-hidden="true" /> {{ t('items.editor.addOption') }}
          </button>
          <div v-if="form.options.length > 0" class="flex flex-wrap items-center gap-2 pt-1">
            <span class="text-sm">{{ t('items.editor.doneWhen') }}</span>
            <select v-model="form.completionRule" class="select select-bordered select-sm">
              <option value="">{{ t('items.editor.doneAny') }}</option>
              <option value="all">{{ t('items.editor.doneAll') }}</option>
              <option value="at_least_n">{{ t('items.editor.doneAtLeastN') }}</option>
            </select>
            <input
              v-if="form.completionRule === 'at_least_n'"
              v-model="form.completionN"
              inputmode="numeric"
              class="input input-bordered input-sm w-16"
            />
          </div>
        </fieldset>
      </div>

      <!-- Section 3: Schedule & Cadence -->
      <div class="card card-border bg-base-100 border-base-300 p-5 shadow-xs space-y-4">
        <h2
          class="text-xs font-semibold uppercase tracking-wider text-base-content/70 flex items-center gap-2"
        >
          <Icon icon="lucide:calendar" class="size-4 text-primary" aria-hidden="true" />
          {{ t('items.editor.section.schedule') }}
        </h2>

        <fieldset class="flex flex-col gap-3">
          <legend class="sr-only">{{ t('items.editor.schedule') }}</legend>
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <label class="flex flex-col gap-1.5">
              <span class="text-sm font-medium">{{ t('items.editor.frequency') }}</span>
              <select
                v-model="form.scheduleType"
                class="select select-bordered w-full"
                data-testid="f-schedule"
              >
                <option
                  v-for="s in allowedSchedules(form.trackerType, form.polarity)"
                  :key="s"
                  :value="s"
                >
                  {{ t(`items.editor.sched.${s}`) }}
                </option>
              </select>
              <p
                v-if="form.scheduleType === 'none'"
                class="text-xs text-base-content/60 pt-1"
                data-testid="sched-none-hint"
              >
                {{ t('items.editor.schedNoneHint') }}
              </p>
            </label>

            <label v-if="form.scheduleType === 'fixed_rrule'" class="flex flex-col gap-1.5">
              <span class="text-sm font-medium">{{ t('items.editor.schedule') }}</span>
              <select
                v-model="form.periodKind"
                class="select select-bordered w-full"
                data-testid="f-period"
              >
                <option value="daily">{{ t('items.editor.period.daily') }}</option>
                <option value="weekly">{{ t('items.editor.period.weekly') }}</option>
                <option value="monthly">{{ t('items.editor.period.monthly') }}</option>
                <option value="yearly">{{ t('items.editor.period.yearly') }}</option>
                <option value="interval">{{ t('items.editor.period.interval') }}</option>
                <option value="custom">{{ t('items.editor.period.custom') }}</option>
                <option value="once" class="hidden">{{ t('items.editor.period.once') }}</option>
              </select>
            </label>
          </div>

          <template v-if="form.scheduleType === 'fixed_rrule'">
            <label
              v-if="form.periodKind === 'interval'"
              class="flex items-center gap-2 text-sm pt-1"
            >
              <template v-for="(p, i) in tParts('items.editor.everyNDays')" :key="i">
                <input
                  v-if="'slot' in p"
                  v-model="form.interval"
                  inputmode="numeric"
                  class="input input-bordered input-sm w-20"
                  data-testid="f-interval"
                />
                <template v-else>{{ p.text }}</template>
              </template>
            </label>
            <div v-if="form.periodKind === 'weekly'" class="flex flex-wrap gap-1.5 pt-1">
              <button
                v-for="d in WEEKDAYS"
                :key="d"
                type="button"
                class="btn btn-sm rounded-full"
                :class="
                  form.weekDays.includes(d)
                    ? 'btn-primary shadow-xs'
                    : 'btn-ghost bg-base-200/70 border-base-300'
                "
                :aria-pressed="form.weekDays.includes(d)"
                :data-testid="`wd-${d}`"
                @click="toggleDay(d)"
              >
                {{ weekdayName(WD_INDEX[d], 'short') }}
              </button>
            </div>
            <label
              v-if="form.periodKind === 'monthly'"
              class="flex items-center gap-2 text-sm pt-1"
            >
              <template v-for="(p, i) in tParts('items.editor.monthDayLine')" :key="i">
                <select
                  v-if="'slot' in p"
                  v-model="form.monthDay"
                  class="select select-bordered select-sm"
                  data-testid="f-monthday"
                >
                  <option v-for="n in 31" :key="n" :value="String(n)">
                    {{ t('items.editor.monthDayOption', { n }) }}
                  </option>
                  <option value="-1">{{ t('items.editor.lastDay') }}</option>
                </select>
                <template v-else>{{ p.text }}</template>
              </template>
            </label>
            <div v-if="form.periodKind === 'yearly'" class="flex items-center gap-2 pt-1">
              <select
                v-model="form.yearMonth"
                class="select select-bordered select-sm"
                :aria-label="t('items.editor.month')"
              >
                <option v-for="m in 12" :key="m" :value="String(m)">
                  {{ monthName(m, 'short') }}
                </option>
              </select>
              <select
                v-model="form.yearDay"
                class="select select-bordered select-sm"
                :aria-label="t('items.editor.day')"
              >
                <option v-for="n in 31" :key="n" :value="String(n)">
                  {{ t('items.editor.monthDayOption', { n }) }}
                </option>
              </select>
            </div>
            <label v-if="form.periodKind === 'custom'" class="flex flex-col gap-1 pt-1">
              <span class="text-xs text-base-content/70">{{ t('items.editor.customHint') }}</span>
              <input
                v-model="form.custom"
                class="input input-bordered font-mono text-sm"
                spellcheck="false"
                data-testid="f-custom"
              />
            </label>
          </template>

          <div v-if="form.scheduleType === 'rolling'" class="flex flex-wrap items-center gap-2">
            <template v-for="(p, i) in tParts('items.editor.rollingLine')" :key="i">
              <input
                v-if="'slot' in p && p.slot === 'input'"
                v-model="form.rollingInterval"
                inputmode="numeric"
                class="input input-bordered input-sm w-20"
                data-testid="f-rolling"
              />
              <select
                v-else-if="'slot' in p"
                v-model="form.rollingUnit"
                class="select select-bordered select-sm"
              >
                <option value="day">{{ t('items.editor.unitDays') }}</option>
                <option value="month">{{ t('items.editor.unitMonths') }}</option>
                <option value="year">{{ t('items.editor.unitYears') }}</option>
              </select>
              <template v-else>{{ p.text }}</template>
            </template>
          </div>

          <div
            v-if="form.scheduleType === 'quota'"
            class="flex flex-col gap-2.5 rounded-lg border border-base-300 bg-base-200/40 p-3"
            data-testid="quota"
          >
            <div class="flex flex-wrap items-center gap-2">
              <select
                v-model="form.quotaComparator"
                class="select select-bordered select-sm"
                :disabled="form.polarity === 'negative'"
              >
                <option value="at_least">{{ t('items.editor.atLeast') }}</option>
                <option value="at_most">{{ t('items.editor.atMost') }}</option>
              </select>
              <input
                v-model="form.quotaCount"
                inputmode="decimal"
                class="input input-bordered input-sm w-20"
                data-testid="f-quota-count"
              />
              <select v-model="form.quotaMeasure" class="select select-bordered select-sm">
                <option v-for="m in allowedMeasures(form)" :key="m" :value="m">
                  {{ t(`items.editor.measure.${m}`) }}
                </option>
              </select>
            </div>
            <div class="flex flex-wrap items-center gap-2">
              <span class="text-sm">{{ t('items.editor.per') }}</span>
              <select v-model="form.quotaPeriod" class="select select-bordered select-sm">
                <option value="week">{{ t('today.quota.week') }}</option>
                <option value="month">{{ t('today.quota.month') }}</option>
                <option value="year">{{ t('today.quota.year') }}</option>
              </select>
              <select v-model="form.quotaWindow" class="select select-bordered select-sm">
                <option value="calendar">{{ t('items.editor.calendarPeriod') }}</option>
                <option value="rolling">{{ t('items.editor.rollingWindow') }}</option>
              </select>
            </div>
            <label
              v-if="quotaNeedsOption(form)"
              class="flex items-center gap-2 text-sm pt-1 border-t border-base-300/60"
            >
              {{ t('items.editor.countOption') }}
              <select v-model="form.quotaOption" class="select select-bordered select-sm">
                <option :value="null">{{ t('items.editor.choose') }}</option>
                <option v-for="(o, i) in form.options" :key="i" :value="i">
                  {{ o.name || t('items.editor.optionN', { n: i + 1 }) }}
                </option>
              </select>
            </label>
          </div>

          <!-- Start Date and Reminder Time -->
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 pt-2">
            <label class="flex flex-col gap-1.5">
              <span class="text-sm font-medium">{{ t('items.editor.startsOn') }}</span>
              <DateInput v-model="form.dtstart" data-testid="f-starts-on" />
            </label>
            <label class="flex flex-col gap-1.5">
              <span class="text-sm font-medium">{{ t('items.editor.remindTime') }}</span>
              <input
                v-model="form.remindTime"
                type="time"
                class="input input-bordered w-full"
                data-testid="f-remind"
              />
            </label>
          </div>
        </fieldset>
      </div>

      <!-- Section 4: More Settings (Collapsible) -->
      <details class="card card-border bg-base-100 border-base-300 shadow-xs group">
        <summary
          class="p-4 cursor-pointer font-medium flex items-center justify-between text-sm text-base-content/70 hover:text-base-content select-none"
        >
          <span class="flex items-center gap-2">
            <Icon icon="lucide:sliders-horizontal" class="size-4 text-primary" aria-hidden="true" />
            {{ t('items.editor.advancedSection') }}
          </span>
          <Icon
            icon="lucide:chevron-down"
            class="size-4 opacity-60 transition-transform duration-200 group-open:rotate-180"
            aria-hidden="true"
          />
        </summary>
        <div class="px-5 pb-5 pt-2 space-y-4 border-t border-base-300/60">
          <!-- Ends on date -->
          <label v-if="form.scheduleType === 'fixed_rrule'" class="flex items-center gap-2 text-sm">
            <span class="w-24 shrink-0 font-medium">{{ t('items.editor.endsOn') }}</span>
            <DateInput v-model="form.until" clearable data-testid="f-ends-on" />
          </label>

          <!-- Holiday Mode -->
          <fieldset
            v-if="showHoliday"
            class="flex flex-col gap-2 rounded-lg border border-base-300 bg-base-200/40 p-3"
            data-testid="holiday"
          >
            <legend class="text-xs font-semibold uppercase tracking-wider text-base-content/70">
              {{ t('items.editor.holidays') }}
            </legend>
            <label class="flex items-center gap-2 cursor-pointer text-sm">
              <input
                v-model="form.holidayMode"
                type="radio"
                value="none"
                class="radio radio-sm radio-primary"
              />
              {{ t('items.editor.holidayNone') }}
            </label>
            <label class="flex items-center gap-2 cursor-pointer text-sm">
              <input
                v-model="form.holidayMode"
                type="radio"
                value="skip_holidays"
                class="radio radio-sm radio-primary"
              />
              {{ t('items.editor.holidaySkip') }}
            </label>
            <label class="flex items-center gap-2 cursor-pointer text-sm">
              <input
                v-model="form.holidayMode"
                type="radio"
                value="workdays_only"
                class="radio radio-sm radio-primary"
              />
              {{ t('items.editor.holidayWorkdays') }}
            </label>
          </fieldset>

          <!-- Stale / Warning Days -->
          <div v-if="form.scheduleType === 'none'" class="flex flex-col gap-1">
            <label class="flex items-center gap-2 text-sm">
              <template v-for="(p, i) in tParts('items.editor.staleLine')" :key="i">
                <input
                  v-if="'slot' in p"
                  v-model="form.staleAfterDays"
                  inputmode="numeric"
                  class="input input-bordered input-sm w-20"
                />
                <template v-else>{{ p.text }}</template>
              </template>
            </label>
            <span class="text-xs text-base-content/60">{{ t('items.editor.staleHint') }}</span>
          </div>

          <label v-if="form.trackerType === 'maintenance'" class="flex items-center gap-2 text-sm">
            <template v-for="(p, i) in tParts('items.editor.warnLine')" :key="i">
              <input
                v-if="'slot' in p"
                v-model="form.warningDays"
                inputmode="numeric"
                class="input input-bordered input-sm w-20"
              />
              <template v-else>{{ p.text }}</template>
            </template>
          </label>
        </div>
      </details>

      <!-- Common Section: Live Schedule Preview -->
      <section
        class="card card-border bg-base-200/50 border-base-300 p-4 shadow-xs space-y-2"
        aria-live="polite"
        data-testid="preview"
      >
        <h2
          class="text-xs font-semibold uppercase tracking-wider text-base-content/70 flex items-center gap-2"
        >
          <Icon icon="lucide:sparkles" class="size-4 text-primary" aria-hidden="true" />
          {{ t('items.editor.section.preview') }}
        </h2>
        <p class="font-medium text-sm" data-testid="preview-sentence">{{ preview.sentence }}</p>
        <ul
          v-if="preview.dates && preview.dates.length"
          class="flex flex-wrap gap-1.5 text-xs pt-1"
          data-testid="preview-dates"
        >
          <li
            v-for="d in preview.dates"
            :key="d"
            class="badge badge-neutral badge-sm font-mono text-[11px]"
          >
            {{ formatCustomDate(d) }}
          </li>
        </ul>
        <p v-if="preview.note" class="text-xs opacity-60">{{ preview.note }}</p>
      </section>

      <!-- Section: Migration (edit mode) -->
      <section
        v-if="isEdit"
        class="card card-border bg-base-100 border-base-300 p-5 shadow-xs space-y-3"
      >
        <h2
          class="text-xs font-semibold uppercase tracking-wider text-base-content/70 flex items-center gap-2"
        >
          <Icon icon="lucide:calendar-days" class="size-4 text-primary" aria-hidden="true" />
          {{ t('items.editor.section.migration') }}
        </h2>
        <p
          v-if="ruleWillChange"
          class="alert alert-info text-sm py-2"
          role="status"
          data-testid="rule-warning"
        >
          {{
            !retro
              ? t('items.editor.ruleWarningToday')
              : effectiveFrom
                ? t('items.editor.ruleWarningDate', { date: formatCustomDate(effectiveFrom) })
                : t('items.editor.ruleWarningBelow')
          }}
        </p>
        <label class="flex items-center gap-2 text-sm cursor-pointer select-none">
          <input
            v-model="retro"
            type="checkbox"
            class="checkbox checkbox-sm checkbox-primary"
            data-testid="f-retro"
          />
          {{ t('items.editor.retro') }}
        </label>
        <DateInput v-if="retro" v-model="effectiveFrom" :max="today" data-testid="f-effective" />
      </section>

      <ul
        v-if="showErrors && errors.length"
        class="alert alert-error flex-col items-start text-sm py-3"
        role="alert"
        data-testid="form-errors"
      >
        <li v-for="e in errors" :key="e">{{ e }}</li>
      </ul>
      <p
        v-if="submitError"
        class="alert alert-error text-sm py-2"
        role="alert"
        data-testid="submit-error"
      >
        {{ submitError }}
      </p>

      <div class="flex items-center gap-3 pt-2">
        <button type="submit" class="btn btn-primary shadow-xs" :disabled="busy" data-testid="save">
          {{ isEdit ? t('items.editor.saveChanges') : t('items.editor.create') }}
        </button>
        <RouterLink to="/" class="btn btn-ghost">{{ t('common.cancel') }}</RouterLink>
        <div v-if="isEdit" class="ml-auto flex items-center gap-2">
          <button
            v-if="!confirmDelete"
            type="button"
            class="btn btn-outline btn-error"
            :disabled="busy"
            data-testid="delete"
            @click="confirmDelete = true"
          >
            <Icon icon="lucide:trash-2" aria-hidden="true" /> {{ t('items.detail.deleteItem') }}
          </button>
          <template v-else>
            <span class="text-sm text-error font-medium">{{ t('items.detail.deleteAsk') }}</span>
            <button
              type="button"
              class="btn btn-error shadow-xs"
              :disabled="busy"
              data-testid="delete-confirm"
              @click="removeItem"
            >
              {{ t('items.detail.deleteConfirm') }}
            </button>
            <button type="button" class="btn btn-ghost" @click="confirmDelete = false">
              {{ t('common.cancel') }}
            </button>
          </template>
        </div>
      </div>
    </form>
  </section>
</template>
