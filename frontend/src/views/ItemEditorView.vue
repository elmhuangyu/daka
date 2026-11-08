<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { Icon } from '@iconify/vue'
import { useRoute, useRouter } from 'vue-router'
import {
  createCategory, deleteOption, getItem, listCategories, patchItem, postItem, postOption, putOption,
  type CategoryDto, type ItemDto,
} from '../api'
import {
  allowedMeasures, allowedPolarities, allowedSchedules, buildCreateBody, buildEditPlan, describeItemError,
  emptyForm, formFromItem, holidayAllowed, isNumeric, normalizeForm, optionRequest, previewRule,
  quotaNeedsOption, validateForm, type ItemForm,
} from '../itemForm'
import { localIso, WEEKDAYS } from '../rrule'
import { usePrivacyStore } from '../stores/privacy'

const route = useRoute()
const router = useRouter()
const privacy = usePrivacyStore()

const editId = computed(() => (route.params.id ? Number(route.params.id) : null))
const isEdit = computed(() => editId.value !== null)

const form = reactive<ItemForm>(emptyForm())
const original = ref<ItemDto | null>(null)
const categories = ref<CategoryDto[]>([])
const loading = ref(false)
const busy = ref(false)
const loadError = ref('')
const submitError = ref('')
const showErrors = ref(false)
const retro = ref(false)
const effectiveFrom = ref('')
const newCategory = ref('')
const newCategorySensitive = ref(false)
const today = localIso()

const TRACKERS = [
  { v: 'boolean', label: 'Yes / no' },
  { v: 'counter', label: 'Counter' },
  { v: 'metric', label: 'Metric' },
  { v: 'maintenance', label: 'Maintenance' },
] as const
const SCHEDULES = {
  fixed_rrule: 'Repeats on a pattern',
  rolling: 'Rolling (after last done)',
  quota: 'Quota (N per period)',
  none: 'Unscheduled',
} as const
const WD_LABEL: Record<string, string> = { MO: 'Mon', TU: 'Tue', WE: 'Wed', TH: 'Thu', FR: 'Fri', SA: 'Sat', SU: 'Sun' }

// Editing an existing item needs plaintext; with the privacy guard on the
// server redacts names, so saving would overwrite them. Block instead.
const blocked = computed(() => isEdit.value && privacy.redact)

async function load() {
  loadError.value = ''
  if (blocked.value) return
  loading.value = true
  try {
    const [cats, item] = await Promise.all([
      privacy.redact ? Promise.resolve([]) : listCategories(),
      isEdit.value ? getItem(editId.value!) : Promise.resolve(null),
    ])
    categories.value = cats
    if (item) {
      original.value = item
      Object.assign(form, formFromItem(item))
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
      original.value = null
      Object.assign(form, emptyForm())
    } else {
      void load()
    }
  },
)

watch(
  () => [form.trackerType, form.polarity, form.scheduleType, form.quotaMeasure, form.options.length],
  () => normalizeForm(form),
)

const errors = computed(() => validateForm(form))
const preview = computed(() => {
  const o = original.value
  const keepPhase = o && o.current_rule.schedule_type === 'fixed_rrule' && o.current_rule.dtstart
  return previewRule(form, today, keepPhase ? o.current_rule.dtstart : today)
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
  if (form.quotaOption !== null) form.quotaOption = form.quotaOption === i ? null : form.quotaOption > i ? form.quotaOption - 1 : form.quotaOption
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
  } catch (e) {
    submitError.value = describeItemError(e)
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
  const item = await postItem(buildCreateBody(form))
  if (form.remindTime !== '') {
    try {
      await patchItem(item.id, { remind_time: form.remindTime })
    } catch (e) {
      // The item exists; continue in edit mode so the reminder can be retried.
      await router.replace({ name: 'item-edit', params: { id: item.id } })
      submitError.value = `Item created, but the reminder time was not saved: ${describeItemError(e)}`
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
  for (const i of plan.updateOptions) await putOption(orig.id, form.options[i].id!, optionRequest(form.options[i], i))
  const rule = plan.patch.rule as Record<string, unknown> | undefined
  if (rule && form.quotaOption !== null && quotaNeedsOption(form)) rule.quota_option_id = optionIds.get(form.quotaOption)
  if (plan.hasPatch) await patchItem(orig.id, plan.patch)
  for (const id of plan.removeOptionIds) await deleteOption(orig.id, id)
  await router.push('/')
}

const showHoliday = computed(() => holidayAllowed(form))
</script>

<template>
  <section class="mx-auto max-w-2xl p-4" data-testid="item-editor">
    <h1 class="mb-4 text-2xl font-bold">{{ isEdit ? 'Edit item' : 'New item' }}</h1>

    <p v-if="blocked" class="alert alert-warning" role="alert" data-testid="privacy-blocked">
      The privacy guard is on. Turn it off to edit this item.
    </p>
    <p v-else-if="loadError" class="alert alert-error" role="alert">{{ loadError }}</p>
    <p v-else-if="loading" class="opacity-70">Loading...</p>

    <form v-else class="flex flex-col gap-5" novalidate @submit.prevent="submit">
      <label class="flex flex-col gap-1">
        <span class="label-text font-medium">Name</span>
        <input v-model="form.name" class="input input-bordered w-full" maxlength="200" autocomplete="off" data-testid="f-name" />
      </label>

      <fieldset class="flex flex-col gap-2" :disabled="isEdit">
        <legend class="label-text font-medium">Type</legend>
        <div class="join flex-wrap">
          <button
            v-for="t in TRACKERS" :key="t.v" type="button" class="btn join-item btn-sm"
            :class="{ 'btn-primary': form.trackerType === t.v }" :data-testid="`tracker-${t.v}`"
            @click="form.trackerType = t.v"
          >{{ t.label }}</button>
        </div>
        <label v-if="allowedPolarities(form.trackerType).length > 1" class="flex items-center gap-2">
          <input v-model="form.polarity" type="radio" value="positive" class="radio radio-sm" /> Build it up (do more)
          <input v-model="form.polarity" type="radio" value="negative" class="radio radio-sm ml-3" data-testid="polarity-negative" /> Limit it (do less)
        </label>
        <p v-if="isEdit" class="text-xs opacity-70">Type and polarity cannot be changed after creation.</p>
      </fieldset>

      <fieldset v-if="isNumeric(form.trackerType)" class="grid grid-cols-2 gap-2">
        <legend class="label-text mb-1 font-medium">Measurement</legend>
        <label class="flex flex-col gap-1"><span class="text-xs">Unit</span>
          <input v-model="form.unit" class="input input-bordered input-sm" maxlength="32" /></label>
        <label class="flex flex-col gap-1"><span class="text-xs">Step</span>
          <input v-model="form.step" inputmode="decimal" class="input input-bordered input-sm" /></label>
        <label class="flex flex-col gap-1"><span class="text-xs">Min</span>
          <input v-model="form.minVal" inputmode="decimal" class="input input-bordered input-sm" /></label>
        <label class="flex flex-col gap-1"><span class="text-xs">Max</span>
          <input v-model="form.maxVal" inputmode="decimal" class="input input-bordered input-sm" /></label>
        <label v-if="form.trackerType === 'metric'" class="col-span-2 flex flex-col gap-1">
          <span class="text-xs">Daily value is the</span>
          <select v-model="form.aggregate" class="select select-bordered select-sm">
            <option value="">Default</option><option value="sum">Sum</option>
            <option value="latest">Latest</option><option value="avg">Average</option>
          </select>
        </label>
        <label v-if="form.options.length === 0" class="col-span-2 flex flex-col gap-1">
          <span class="text-xs">Daily target (optional)</span>
          <input v-model="form.target" inputmode="decimal" class="input input-bordered input-sm" data-testid="f-target" />
        </label>
      </fieldset>

      <fieldset v-if="isNumeric(form.trackerType)" class="flex flex-col gap-2">
        <legend class="label-text font-medium">Options (optional)</legend>
        <div v-for="(o, i) in form.options" :key="i" class="flex flex-wrap items-end gap-2 rounded border border-base-300 p-2">
          <label class="flex min-w-32 flex-1 flex-col gap-1"><span class="text-xs">Name</span>
            <input v-model="o.name" class="input input-bordered input-sm" maxlength="200" :data-testid="`opt-name-${i}`" /></label>
          <label class="flex w-20 flex-col gap-1"><span class="text-xs">Unit</span>
            <input v-model="o.unit" class="input input-bordered input-sm" /></label>
          <label class="flex w-20 flex-col gap-1"><span class="text-xs">Step</span>
            <input v-model="o.step" inputmode="decimal" class="input input-bordered input-sm" /></label>
          <label class="flex w-24 flex-col gap-1"><span class="text-xs">Target</span>
            <input v-model="o.target" inputmode="decimal" class="input input-bordered input-sm" /></label>
          <button type="button" class="btn btn-ghost btn-sm btn-square" aria-label="Remove option" @click="removeOption(i)">
            <Icon icon="lucide:trash-2" aria-hidden="true" />
          </button>
        </div>
        <button type="button" class="btn btn-sm btn-outline self-start" data-testid="add-option" @click="addOption">
          <Icon icon="lucide:plus" aria-hidden="true" /> Add option
        </button>
        <div v-if="form.options.length > 0" class="flex flex-wrap items-center gap-2">
          <span class="text-sm">Done when</span>
          <select v-model="form.completionRule" class="select select-bordered select-sm">
            <option value="">any option meets its target</option>
            <option value="all">all options meet their targets</option>
            <option value="at_least_n">at least N options meet their targets</option>
          </select>
          <input v-if="form.completionRule === 'at_least_n'" v-model="form.completionN" inputmode="numeric" class="input input-bordered input-sm w-16" />
        </div>
      </fieldset>

      <fieldset class="flex flex-col gap-3">
        <legend class="label-text font-medium">Schedule</legend>
        <select v-model="form.scheduleType" class="select select-bordered" data-testid="f-schedule">
          <option v-for="s in allowedSchedules(form.trackerType, form.polarity)" :key="s" :value="s">{{ SCHEDULES[s] }}</option>
        </select>

        <template v-if="form.scheduleType === 'fixed_rrule'">
          <select v-model="form.periodKind" class="select select-bordered" data-testid="f-period">
            <option value="daily">Every day</option>
            <option value="interval">Every N days</option>
            <option value="weekly">On certain weekdays</option>
            <option value="monthly">On a day of the month</option>
            <option value="yearly">On a date each year</option>
            <option value="custom">Custom RRULE (advanced)</option>
          </select>
          <label v-if="form.periodKind === 'interval'" class="flex items-center gap-2">Every
            <input v-model="form.interval" inputmode="numeric" class="input input-bordered input-sm w-20" data-testid="f-interval" /> days</label>
          <div v-if="form.periodKind === 'weekly'" class="flex flex-wrap gap-1">
            <button
              v-for="d in WEEKDAYS" :key="d" type="button" class="btn btn-sm"
              :class="{ 'btn-primary': form.weekDays.includes(d) }" :aria-pressed="form.weekDays.includes(d)"
              :data-testid="`wd-${d}`" @click="toggleDay(d)"
            >{{ WD_LABEL[d] }}</button>
          </div>
          <label v-if="form.periodKind === 'monthly'" class="flex items-center gap-2">On day
            <select v-model="form.monthDay" class="select select-bordered select-sm" data-testid="f-monthday">
              <option v-for="n in 31" :key="n" :value="String(n)">{{ n }}</option>
              <option value="-1">last day</option>
            </select> of every month</label>
          <div v-if="form.periodKind === 'yearly'" class="flex items-center gap-2">
            <select v-model="form.yearMonth" class="select select-bordered select-sm" aria-label="Month">
              <option v-for="(m, i) in ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec']" :key="m" :value="String(i + 1)">{{ m }}</option>
            </select>
            <select v-model="form.yearDay" class="select select-bordered select-sm" aria-label="Day">
              <option v-for="n in 31" :key="n" :value="String(n)">{{ n }}</option>
            </select>
          </div>
          <label v-if="form.periodKind === 'custom'" class="flex flex-col gap-1">
            <span class="text-xs">RRULE, for example FREQ=MONTHLY;BYDAY=MO;BYSETPOS=1</span>
            <input v-model="form.custom" class="input input-bordered font-mono" spellcheck="false" data-testid="f-custom" />
          </label>
          <label class="flex items-center gap-2 text-sm">Ends on (optional)
            <input v-model="form.until" type="date" class="input input-bordered input-sm" /></label>
          <fieldset v-if="showHoliday" class="flex flex-col gap-1" data-testid="holiday">
            <legend class="text-sm font-medium">Holidays</legend>
            <label class="flex items-center gap-2"><input v-model="form.holidayMode" type="radio" value="none" class="radio radio-sm" /> Ignore holidays</label>
            <label class="flex items-center gap-2"><input v-model="form.holidayMode" type="radio" value="skip_holidays" class="radio radio-sm" /> Skip statutory holidays</label>
            <label class="flex items-center gap-2"><input v-model="form.holidayMode" type="radio" value="workdays_only" class="radio radio-sm" /> Workdays only (move to the next workday)</label>
          </fieldset>
        </template>

        <div v-if="form.scheduleType === 'rolling'" class="flex items-center gap-2">Due every
          <input v-model="form.rollingInterval" inputmode="numeric" class="input input-bordered input-sm w-20" data-testid="f-rolling" />
          <select v-model="form.rollingUnit" class="select select-bordered select-sm">
            <option value="day">days</option><option value="month">months</option><option value="year">years</option>
          </select> after the last completion</div>

        <div v-if="form.scheduleType === 'quota'" class="flex flex-col gap-2" data-testid="quota">
          <div class="flex flex-wrap items-center gap-2">
            <select v-model="form.quotaComparator" class="select select-bordered select-sm" :disabled="form.polarity === 'negative'">
              <option value="at_least">At least</option><option value="at_most">At most</option>
            </select>
            <input v-model="form.quotaCount" inputmode="decimal" class="input input-bordered input-sm w-20" data-testid="f-quota-count" />
            <select v-model="form.quotaMeasure" class="select select-bordered select-sm">
              <option v-for="m in allowedMeasures(form)" :key="m" :value="m">{{ m === 'days' ? 'days' : m === 'occurrences' ? 'times' : 'total amount' }}</option>
            </select>
          </div>
          <div class="flex flex-wrap items-center gap-2">per
            <select v-model="form.quotaPeriod" class="select select-bordered select-sm">
              <option value="week">week</option><option value="month">month</option><option value="year">year</option>
            </select>
            <select v-model="form.quotaWindow" class="select select-bordered select-sm">
              <option value="calendar">calendar period</option><option value="rolling">rolling window</option>
            </select>
          </div>
          <label v-if="quotaNeedsOption(form)" class="flex items-center gap-2 text-sm">Count option
            <select v-model="form.quotaOption" class="select select-bordered select-sm">
              <option :value="null">Choose...</option>
              <option v-for="(o, i) in form.options" :key="i" :value="i">{{ o.name || `Option ${i + 1}` }}</option>
            </select></label>
        </div>

        <label v-if="form.scheduleType === 'none'" class="flex items-center gap-2 text-sm">Flag as stale after
          <input v-model="form.staleAfterDays" inputmode="numeric" class="input input-bordered input-sm w-20" /> days without activity (0 = never)</label>
        <label v-if="form.trackerType === 'maintenance'" class="flex items-center gap-2 text-sm">Warn
          <input v-model="form.warningDays" inputmode="numeric" class="input input-bordered input-sm w-20" /> days before due</label>
      </fieldset>

      <section class="rounded border border-base-300 bg-base-200 p-3" aria-live="polite" data-testid="preview">
        <h2 class="text-sm font-semibold">Preview</h2>
        <p data-testid="preview-sentence">{{ preview.sentence }}</p>
        <ul v-if="preview.dates && preview.dates.length" class="mt-1 text-sm" data-testid="preview-dates">
          <li v-for="d in preview.dates" :key="d">{{ d }}</li>
        </ul>
        <p v-if="preview.note" class="mt-1 text-xs opacity-70">{{ preview.note }}</p>
      </section>

      <div class="grid grid-cols-2 gap-3">
        <label class="flex flex-col gap-1"><span class="label-text font-medium">Reminder time</span>
          <input v-model="form.remindTime" type="time" class="input input-bordered" data-testid="f-remind" /></label>
        <label class="flex items-end gap-2 pb-3"><input v-model="form.isSensitive" type="checkbox" class="checkbox" data-testid="f-sensitive" /> Sensitive</label>
      </div>

      <fieldset v-if="!privacy.redact" class="flex flex-col gap-2">
        <legend class="label-text font-medium">Category</legend>
        <select v-model="form.categoryId" class="select select-bordered" data-testid="f-category">
          <option :value="null">No category</option>
          <option v-for="c in categories" :key="c.id" :value="c.id">{{ c.name }}{{ c.is_sensitive ? ' (sensitive)' : '' }}</option>
        </select>
        <div class="flex flex-wrap items-center gap-2">
          <input v-model="newCategory" class="input input-bordered input-sm" placeholder="New category" maxlength="200" data-testid="f-new-category" />
          <label class="flex items-center gap-1 text-sm"><input v-model="newCategorySensitive" type="checkbox" class="checkbox checkbox-sm" /> Sensitive</label>
          <button type="button" class="btn btn-sm" data-testid="add-category" @click="addCategory">Add</button>
        </div>
      </fieldset>

      <section v-if="isEdit" class="flex flex-col gap-2 rounded border border-base-300 p-3">
        <p v-if="ruleWillChange" class="alert alert-info text-sm" role="status" data-testid="rule-warning">
          Changing the schedule or targets creates a new rule version
          {{ retro ? `effective ${effectiveFrom || 'from the date below'}` : 'effective today' }}. Earlier days keep the old rule.
        </p>
        <label class="flex items-center gap-2 text-sm"><input v-model="retro" type="checkbox" class="checkbox checkbox-sm" data-testid="f-retro" /> Apply from an earlier date</label>
        <input v-if="retro" v-model="effectiveFrom" type="date" :max="today" class="input input-bordered input-sm w-48" data-testid="f-effective" />
      </section>

      <ul v-if="showErrors && errors.length" class="alert alert-error flex-col items-start text-sm" role="alert" data-testid="form-errors">
        <li v-for="e in errors" :key="e">{{ e }}</li>
      </ul>
      <p v-if="submitError" class="alert alert-error text-sm" role="alert" data-testid="submit-error">{{ submitError }}</p>

      <div class="flex gap-2">
        <button type="submit" class="btn btn-primary" :disabled="busy" data-testid="save">{{ isEdit ? 'Save changes' : 'Create item' }}</button>
        <RouterLink to="/" class="btn btn-ghost">Cancel</RouterLink>
      </div>
    </form>
  </section>
</template>
