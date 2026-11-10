import { ApiError, describeError, type ItemDto } from './api'
import { t, tn, type MessageKey, type PluralBase } from './i18n'
import { errorKeyForCode } from './i18n/errors'
import { formatCustomDate } from './i18n/format'
import {
  buildRRule,
  describeRRule,
  nextOccurrences,
  presetFromRRule,
  WEEKDAYS,
  type Preset,
  type Weekday,
} from './rrule'

export type TrackerType = 'boolean' | 'counter' | 'metric' | 'maintenance'
export type Polarity = 'positive' | 'negative'
export type ScheduleType = 'fixed_rrule' | 'rolling' | 'quota' | 'none'
export type PeriodKind = 'daily' | 'interval' | 'weekly' | 'monthly' | 'yearly' | 'custom' | 'once'
export type HolidayMode = 'none' | 'skip_holidays' | 'workdays_only'

export interface OptionForm {
  /** Server id; undefined for an option added in this editing session. */
  id?: number
  name: string
  unit: string
  step: string
  target: string
}

export interface ItemForm {
  name: string
  kind: 'habit' | 'event'
  color: string
  trackerType: TrackerType
  polarity: Polarity
  categoryId: number | null
  projectId: number | null
  isSensitive: boolean
  remindTime: string
  unit: string
  step: string
  minVal: string
  maxVal: string
  aggregate: '' | 'sum' | 'latest' | 'avg'
  target: string
  options: OptionForm[]
  completionRule: '' | 'any' | 'all' | 'at_least_n'
  completionN: string
  warningDays: string
  scheduleType: ScheduleType
  periodKind: PeriodKind
  interval: string
  weekDays: Weekday[]
  monthDay: string
  yearMonth: string
  yearDay: string
  custom: string
  dtstart: string
  until: string
  holidayMode: HolidayMode
  rollingInterval: string
  rollingUnit: 'day' | 'month' | 'year'
  quotaComparator: 'at_least' | 'at_most'
  quotaCount: string
  quotaPeriod: 'week' | 'month' | 'year'
  quotaWindow: 'calendar' | 'rolling'
  quotaMeasure: 'days' | 'occurrences' | 'sum'
  /** Index into `options` of the quota option (sum measure with mixed units). */
  quotaOption: number | null
  staleAfterDays: string
}

export function emptyForm(): ItemForm {
  return {
    name: '',
    kind: 'habit',
    color: '',
    trackerType: 'boolean',
    polarity: 'positive',
    categoryId: null,
    projectId: null,
    isSensitive: false,
    remindTime: '',
    unit: '',
    step: '',
    minVal: '',
    maxVal: '',
    aggregate: '',
    target: '',
    options: [],
    completionRule: '',
    completionN: '',
    warningDays: '',
    scheduleType: 'fixed_rrule',
    periodKind: 'daily',
    interval: '2',
    weekDays: ['MO'],
    monthDay: '1',
    yearMonth: '1',
    yearDay: '1',
    custom: '',
    dtstart: '',
    until: '',
    holidayMode: 'none',
    rollingInterval: '30',
    rollingUnit: 'day',
    quotaComparator: 'at_least',
    quotaCount: '3',
    quotaPeriod: 'week',
    quotaWindow: 'calendar',
    quotaMeasure: 'days',
    quotaOption: null,
    staleAfterDays: '',
  }
}

// ---- which choices are valid (mirrors schedule.ValidateItemRule) ----

export function isNumeric(t: TrackerType): boolean {
  return t === 'counter' || t === 'metric'
}

/** Polarities valid for a tracker type: negative only for boolean and metric. */
export function allowedPolarities(t: TrackerType): Polarity[] {
  return t === 'boolean' || t === 'metric' ? ['positive', 'negative'] : ['positive']
}

export function allowedSchedules(t: TrackerType, p: Polarity): ScheduleType[] {
  if (p === 'negative') return ['quota', 'none']
  if (t === 'maintenance') return ['rolling', 'fixed_rrule', 'none']
  if (isNumeric(t)) return ['fixed_rrule', 'quota', 'none']
  return ['fixed_rrule', 'rolling', 'quota', 'none']
}

/** Holiday policy applies to fixed RRULE schedules only. */
export function holidayAllowed(f: ItemForm): boolean {
  return f.scheduleType === 'fixed_rrule'
}

export function allowedMeasures(f: ItemForm): ('days' | 'occurrences' | 'sum')[] {
  return isNumeric(f.trackerType) ? ['days', 'occurrences', 'sum'] : ['days', 'occurrences']
}

/** Sum over options with different units needs a chosen option. */
export function quotaNeedsOption(f: ItemForm): boolean {
  if (f.scheduleType !== 'quota' || f.quotaMeasure !== 'sum') return false
  return new Set(f.options.map((o) => o.unit.trim())).size > 1
}

/** Repairs choices that became invalid after a tracker/polarity/schedule change. */
export function normalizeForm(f: ItemForm): ItemForm {
  if (!allowedPolarities(f.trackerType).includes(f.polarity)) f.polarity = 'positive'
  const scheds = allowedSchedules(f.trackerType, f.polarity)
  if (!scheds.includes(f.scheduleType)) f.scheduleType = scheds[0]
  if (f.polarity === 'negative') {
    f.quotaComparator = 'at_most'
    if (f.quotaMeasure === 'days') f.quotaMeasure = 'occurrences'
  }
  if (!allowedMeasures(f).includes(f.quotaMeasure))
    f.quotaMeasure = f.polarity === 'negative' ? 'occurrences' : 'days'
  if (!isNumeric(f.trackerType)) {
    if (f.options.length > 0) f.options = []
    f.target = ''
  }
  if (f.options.length === 0) f.completionRule = ''
  if (!quotaNeedsOption(f)) f.quotaOption = null
  return f
}

// ---- parsing helpers ----

/** '' -> null; invalid -> NaN. */
export function num(s: string): number | null {
  const t = s.trim()
  if (t === '') return null
  const n = Number(t)
  return Number.isFinite(n) ? n : NaN
}

function preset(f: ItemForm): Preset {
  switch (f.periodKind) {
    case 'daily':
      return { kind: 'days', interval: 1 }
    case 'interval':
      return { kind: 'days', interval: Number(f.interval) }
    case 'weekly':
      return { kind: 'weekly', days: f.weekDays }
    case 'monthly':
      return { kind: 'monthly', day: Number(f.monthDay) }
    case 'yearly':
      return { kind: 'yearly', month: Number(f.yearMonth), day: Number(f.yearDay) }
    case 'custom':
      return { kind: 'custom', rrule: f.custom }
    case 'once':
      return { kind: 'custom', rrule: 'FREQ=DAILY;COUNT=1' }
  }
}

/** RRULE body for the form's period, or an error message. */
export function formRRule(f: ItemForm): { rrule: string } | { error: string } {
  try {
    return { rrule: buildRRule(preset(f)) }
  } catch (e) {
    return { error: e instanceof Error ? e.message : String(e) }
  }
}

/** Client-side validation; the server stays authoritative. Returns messages in the active locale. */
export function validateForm(f: ItemForm): string[] {
  const errs: string[] = []
  const bad = (s: string) => Number.isNaN(num(s))
  if (f.name.trim() === '') errs.push(t('errors.nameRequired'))
  if (f.remindTime !== '' && !/^([01]\d|2[0-3]):[0-5]\d$/.test(f.remindTime))
    errs.push(t('errors.invalidRemindTime'))
  if (f.scheduleType === 'fixed_rrule') {
    const r = formRRule(f)
    if ('error' in r) errs.push(r.error)
    if (f.until !== '' && !/^\d{4}-\d{2}-\d{2}$/.test(f.until)) errs.push(t('items.err.endDate'))
  }
  if (f.scheduleType === 'rolling') {
    const n = Number(f.rollingInterval)
    if (!Number.isInteger(n) || n < 1) errs.push(t('items.err.rolling'))
  }
  if (f.scheduleType === 'quota') {
    const c = Number(f.quotaCount)
    if (!(c > 0) || !Number.isFinite(c)) errs.push(t('items.err.quotaCount'))
    if (quotaNeedsOption(f) && f.quotaOption === null) errs.push(t('items.err.quotaOption'))
  }
  if (
    [f.step, f.minVal, f.maxVal, f.target, f.warningDays, f.staleAfterDays, f.completionN].some(bad)
  ) {
    errs.push(t('items.err.numeric'))
  }
  for (const o of f.options) {
    if (o.name.trim() === '') errs.push(t('items.err.optionName'))
    if (bad(o.step) || bad(o.target)) errs.push(t('items.err.optionNumbers'))
  }
  if (f.options.length > 0 && f.completionRule === 'at_least_n') {
    const n = Number(f.completionN)
    if (!Number.isInteger(n) || n < 1 || n > f.options.length) errs.push(t('items.err.completionN'))
  }
  return errs
}

// ---- request building ----

type Json = Record<string, unknown>

function drop(o: Json): Json {
  for (const k of Object.keys(o)) if (o[k] === undefined) delete o[k]
  return o
}

/**
 * Schedule/target/completion part of the request body, restricted to the
 * fields valid for the schedule type (the server rejects foreign fields).
 * `patch` sends explicit nulls/zeroes so absent values clear old ones.
 * Option-based items keep per-option targets on the options, not here.
 */
export function buildRule(f: ItemForm, patch: boolean): Json {
  const rule: Json = { schedule_type: f.scheduleType }
  if (f.scheduleType === 'fixed_rrule') {
    const r = formRRule(f)
    rule.rrule_string = 'rrule' in r ? r.rrule : ''
    rule.holiday_mode = f.holidayMode
    if (f.dtstart !== '') rule.dtstart = f.dtstart
    if (f.periodKind === 'once' && f.dtstart !== '') {
      rule.until = f.dtstart
    } else if (f.until !== '') {
      rule.until = f.until
    } else if (patch) {
      rule.until = ''
    }
  } else if (f.scheduleType === 'rolling') {
    rule.rolling_interval = Number(f.rollingInterval)
    rule.rolling_unit = f.rollingUnit
  } else if (f.scheduleType === 'quota') {
    rule.quota_comparator = f.quotaComparator
    rule.quota_count = Number(f.quotaCount)
    rule.quota_period = f.quotaPeriod
    rule.quota_window = f.quotaWindow
    rule.quota_measure = f.quotaMeasure
  } else if (patch || num(f.staleAfterDays) !== null) {
    rule.stale_after_days = num(f.staleAfterDays) ?? 0
  }
  if (isNumeric(f.trackerType) && f.options.length === 0) {
    const t = num(f.target)
    if (t !== null) rule.target_value = t
    else if (patch) rule.target_value = null
  }
  if (f.options.length > 0 && f.completionRule !== '') {
    rule.completion_rule = f.completionRule
    rule.completion_n = f.completionRule === 'at_least_n' ? Number(f.completionN) : 0
  } else if (patch && f.options.length > 0) {
    rule.completion_rule = ''
    rule.completion_n = 0
  }
  return rule
}

function itemNumbers(f: ItemForm): Json {
  if (!isNumeric(f.trackerType)) return {}
  return {
    unit: f.unit.trim(),
    step: num(f.step) ?? 0,
    min_val: num(f.minVal),
    max_val: num(f.maxVal),
    aggregate: f.aggregate,
  }
}

function optionBody(o: OptionForm, i: number): Json {
  return drop({
    name: o.name.trim(),
    unit: o.unit.trim(),
    step: num(o.step) ?? 0,
    sort_order: i,
    target_value: num(o.target) ?? undefined,
  })
}

/** Body for POST /api/items. remind_time is applied by a follow-up PATCH (create does not accept it). */
export function buildCreateBody(f: ItemForm): Json {
  const rule = buildRule(f, false)
  if (f.quotaOption !== null && quotaNeedsOption(f)) rule.quota_option_index = f.quotaOption
  const nums = itemNumbers(f)
  const body: Json = {
    name: f.name.trim(),
    tracker_type: f.trackerType,
    kind: f.kind,
    polarity: f.polarity,
    category_id: f.categoryId,
    project_id: f.projectId,
    is_sensitive: f.isSensitive,
    options: f.options.map(optionBody),
    rule,
  }
  if (f.color) body.color = f.color
  if (isNumeric(f.trackerType)) {
    body.unit = nums.unit
    body.step = nums.step
    if (nums.min_val !== null) body.min_val = nums.min_val
    if (nums.max_val !== null) body.max_val = nums.max_val
    if (nums.aggregate) body.aggregate = nums.aggregate
  }
  if (f.trackerType === 'maintenance' && num(f.warningDays)) body.warning_days = num(f.warningDays)
  return body
}

// ---- edit mode ----

export function formFromItem(it: ItemDto): ItemForm {
  const f = emptyForm()
  const r = it.current_rule
  f.name = it.name
  f.kind = (it.kind as 'habit' | 'event') || 'habit'
  f.color = it.color ?? ''
  f.trackerType = it.tracker_type as TrackerType
  f.polarity = it.polarity as Polarity
  f.categoryId = it.category_id
  f.projectId = it.project_id ?? null
  f.isSensitive = it.is_sensitive
  f.remindTime = it.remind_time ?? ''
  f.unit = it.unit ?? ''
  f.step = it.step ? String(it.step) : ''
  f.minVal = it.min_val != null ? String(it.min_val) : ''
  f.maxVal = it.max_val != null ? String(it.max_val) : ''
  f.aggregate = (it.aggregate ?? '') as ItemForm['aggregate']
  f.warningDays = it.warning_days ? String(it.warning_days) : ''
  f.options = it.options.map((o) => ({
    id: o.id,
    name: o.name,
    unit: o.unit,
    step: o.step ? String(o.step) : '',
    target: o.target_value != null ? String(o.target_value) : '',
  }))
  f.scheduleType = r.schedule_type as ScheduleType
  f.holidayMode = (r.holiday_mode || 'none') as HolidayMode
  f.dtstart = r.dtstart ?? ''
  f.until = r.until ?? ''
  if (r.schedule_type === 'fixed_rrule') {
    if (r.rrule_string === 'FREQ=DAILY;COUNT=1') {
      f.periodKind = 'once'
    } else {
      const p = presetFromRRule(r.rrule_string)
      if (!p) {
        f.periodKind = 'custom'
        f.custom = r.rrule_string
      } else if (p.kind === 'days') {
        f.periodKind = p.interval === 1 ? 'daily' : 'interval'
        f.interval = String(p.interval)
      } else if (p.kind === 'weekly' && (p.interval ?? 1) === 1) {
        f.periodKind = 'weekly'
        f.weekDays = [...p.days]
      } else if (p.kind === 'monthly' && (p.interval ?? 1) === 1) {
        f.periodKind = 'monthly'
        f.monthDay = String(p.day)
      } else if (p.kind === 'yearly') {
        f.periodKind = 'yearly'
        f.yearMonth = String(p.month)
        f.yearDay = String(p.day)
      } else {
        f.periodKind = 'custom'
        f.custom = r.rrule_string
      }
    }
  }
  f.rollingInterval = r.rolling_interval ? String(r.rolling_interval) : f.rollingInterval
  f.rollingUnit = (r.rolling_unit || 'day') as ItemForm['rollingUnit']
  if (r.quota_comparator) f.quotaComparator = r.quota_comparator as ItemForm['quotaComparator']
  if (r.quota_count) f.quotaCount = String(r.quota_count)
  if (r.quota_period) f.quotaPeriod = r.quota_period as ItemForm['quotaPeriod']
  f.quotaWindow = (r.quota_window || 'calendar') as ItemForm['quotaWindow']
  f.quotaMeasure = (r.quota_measure ||
    (it.polarity === 'negative' ? 'occurrences' : 'days')) as ItemForm['quotaMeasure']
  if (r.quota_option_id != null) {
    const i = it.options.findIndex((o) => o.id === r.quota_option_id)
    f.quotaOption = i >= 0 ? i : null
  }
  f.target = r.target_value != null ? String(r.target_value) : ''
  f.completionRule = (r.completion_rule ?? '') as ItemForm['completionRule']
  f.completionN = r.completion_n ? String(r.completion_n) : ''
  f.staleAfterDays = r.stale_after_days ? String(r.stale_after_days) : ''
  return f
}

export interface EditPlan {
  /** Options to create (form index), update (server id) and archive. */
  addOptions: number[]
  updateOptions: number[]
  removeOptionIds: number[]
  /** PATCH body without quota_option_id; the caller resolves it once option ids exist. */
  patch: Json
  /** True when the PATCH carries a rule change (a new version). */
  ruleChanged: boolean
  hasPatch: boolean
}

const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b)

/**
 * Diff of an edited form against the loaded item. Immutable fields
 * (tracker_type, kind, polarity) are never sent. A rule version is only
 * requested when the rule actually changed (or a retroactive date was set).
 */
export function buildEditPlan(
  orig: ItemDto,
  f: ItemForm,
  effectiveFrom: string,
  today: string,
): EditPlan {
  const o = formFromItem(orig)
  const patch: Json = {}
  if (f.name.trim() !== o.name) patch.name = f.name.trim()
  if (f.kind !== o.kind) patch.kind = f.kind
  if (f.color !== o.color) patch.color = f.color
  if (f.categoryId !== o.categoryId) patch.category_id = f.categoryId
  if (f.projectId !== o.projectId) patch.project_id = f.projectId
  if (f.isSensitive !== o.isSensitive) patch.is_sensitive = f.isSensitive
  if (f.remindTime !== o.remindTime) patch.remind_time = f.remindTime === '' ? null : f.remindTime
  if (isNumeric(f.trackerType)) {
    if (f.unit.trim() !== o.unit) patch.unit = f.unit.trim()
    if ((num(f.step) ?? 0) !== (num(o.step) ?? 0)) patch.step = num(f.step) ?? 0
    if (num(f.minVal) !== num(o.minVal)) patch.min_val = num(f.minVal)
    if (num(f.maxVal) !== num(o.maxVal)) patch.max_val = num(f.maxVal)
    if (f.aggregate !== o.aggregate) patch.aggregate = f.aggregate
  }
  if (f.trackerType === 'maintenance' && (num(f.warningDays) ?? 0) !== (num(o.warningDays) ?? 0)) {
    patch.warning_days = num(f.warningDays) ?? 0
  }

  const rule = buildRule(f, true)
  const before = buildRule(o, true)
  const quotaOpt = f.quotaOption !== null && quotaNeedsOption(f) ? f.quotaOption : null
  const ruleDiffers =
    !same(rule, before) || quotaOpt !== (quotaNeedsOption(o) ? o.quotaOption : null)
  const retro = effectiveFrom !== '' && effectiveFrom !== today
  let ruleChanged = false
  if (ruleDiffers || retro) {
    ruleChanged = true
    if (
      f.scheduleType === 'fixed_rrule' &&
      (rule.schedule_type !== before.schedule_type || rule.rrule_string !== before.rrule_string)
    ) {
      rule.dtstart = effectiveFrom || today // restart the INTERVAL phase at the new rule's start
    }
    if (retro) rule.effective_from = effectiveFrom
    if (f.scheduleType === 'quota' && quotaOpt === null) rule.quota_option_id = 0
    patch.rule = rule
  }

  const keep = new Set(f.options.filter((x) => x.id !== undefined).map((x) => x.id))
  const origById = new Map(o.options.map((x) => [x.id, x]))
  const addOptions: number[] = []
  const updateOptions: number[] = []
  f.options.forEach((x, i) => {
    if (x.id === undefined) addOptions.push(i)
    else if (!same(x, origById.get(x.id)) || i !== o.options.findIndex((y) => y.id === x.id))
      updateOptions.push(i)
  })
  return {
    addOptions,
    updateOptions,
    removeOptionIds: o.options.filter((x) => !keep.has(x.id)).map((x) => x.id!),
    patch,
    ruleChanged,
    hasPatch: Object.keys(patch).length > 0,
  }
}

export function optionRequest(o: OptionForm, i: number): Json {
  return optionBody(o, i)
}

// ---- preview ----

export interface RulePreview {
  sentence: string
  /** Upcoming due dates, or null when not computable client-side. */
  dates: string[] | null
  note: string
}

const SPAN: Record<ItemForm['rollingUnit'], PluralBase> = {
  day: 'items.span.day',
  month: 'items.span.month',
  year: 'items.span.year',
}
const PERIOD_KEY = {
  week: 'today.quota.week',
  month: 'today.quota.month',
  year: 'today.quota.year',
} as const

export function previewRule(f: ItemForm, today: string, dtstart?: string): RulePreview {
  switch (f.scheduleType) {
    case 'none':
      return { sentence: t('items.preview.none'), dates: [], note: '' }
    case 'rolling': {
      const n = Number(f.rollingInterval)
      const key =
        f.trackerType === 'maintenance' ? 'items.preview.dueAfter' : 'items.preview.dueAgainAfter'
      return {
        sentence:
          Number.isInteger(n) && n > 0
            ? t(key, { span: tn(SPAN[f.rollingUnit], n) })
            : t('items.preview.invalidInterval'),
        dates: null,
        note: t('items.preview.rollingNote'),
      }
    }
    case 'quota': {
      const c = Number(f.quotaCount)
      const cmp = t(
        f.quotaComparator === 'at_least' ? 'items.preview.atLeast' : 'items.preview.atMost',
      )
      const unit = t(
        f.quotaMeasure === 'sum'
          ? 'items.preview.unitSum'
          : f.quotaMeasure === 'days'
            ? 'items.preview.unitDays'
            : 'items.preview.unitTimes',
        { count: c },
      )
      const period = t(PERIOD_KEY[f.quotaPeriod])
      const window = t(
        f.quotaWindow === 'calendar'
          ? 'items.preview.windowCalendar'
          : 'items.preview.windowRolling',
        { period },
      )
      return {
        sentence:
          c > 0 ? t('items.preview.quota', { cmp, unit, window }) : t('items.preview.invalidQuota'),
        dates: null,
        note: t('items.preview.quotaNote'),
      }
    }
    case 'fixed_rrule': {
      if (f.periodKind === 'once') {
        const d = f.dtstart || dtstart || today
        return {
          sentence: t('items.preview.once', { date: formatCustomDate(d) }),
          dates: [d],
          note: '',
        }
      }
      const r = formRRule(f)
      if ('error' in r) return { sentence: r.error, dates: null, note: '' }
      let sentence = describeRRule(r.rrule)
      if (f.until !== '' && !r.rrule.includes('UNTIL='))
        sentence = t('rrule.until', { head: sentence, date: formatCustomDate(f.until) })
      sentence += t('common.sentenceEnd')
      const dates = nextOccurrences(r.rrule, dtstart || today, today, 5)
      const note =
        f.holidayMode === 'skip_holidays'
          ? t('items.preview.holidaySkip')
          : f.holidayMode === 'workdays_only'
            ? t('items.preview.holidayWorkdays')
            : dates === null
              ? t('items.preview.customUnavailable')
              : ''
      return { sentence, dates, note }
    }
  }
}

// ---- backend reason codes ----

const REASON_KEYS = {
  unknown_tracker_type: 'items.reason.unknown_tracker_type',
  unknown_kind: 'items.reason.unknown_kind',
  unknown_polarity: 'items.reason.unknown_polarity',
  unknown_schedule_type: 'items.reason.unknown_schedule_type',
  schedule_not_allowed_for_tracker: 'items.reason.schedule_not_allowed_for_tracker',
  negative_tracker_not_allowed: 'items.reason.negative_tracker_not_allowed',
  negative_schedule_not_allowed: 'items.reason.negative_schedule_not_allowed',
  invalid_rule: 'items.reason.invalid_rule',
  foreign_rrule_fields: 'items.reason.foreign_rrule_fields',
  foreign_rolling_fields: 'items.reason.foreign_rolling_fields',
  foreign_quota_fields: 'items.reason.foreign_quota_fields',
  holiday_mode_not_allowed: 'items.reason.holiday_mode_not_allowed',
  invalid_quota: 'items.reason.invalid_quota',
  invalid_quota_measure: 'items.reason.invalid_quota_measure',
  quota_option_required: 'items.reason.quota_option_required',
  quota_option_unknown: 'items.reason.quota_option_unknown',
  quota_option_not_applicable: 'items.reason.quota_option_not_applicable',
  invalid_stale_after_days: 'items.reason.invalid_stale_after_days',
  invalid_target_value: 'items.reason.invalid_target_value',
  target_not_applicable: 'items.reason.target_not_applicable',
  options_not_allowed: 'items.reason.options_not_allowed',
  invalid_option: 'items.reason.invalid_option',
  invalid_option_targets: 'items.reason.invalid_option_targets',
  invalid_completion_rule: 'items.reason.invalid_completion_rule',
  completion_rule_not_applicable: 'items.reason.completion_rule_not_applicable',
  invalid_warning_days: 'items.reason.invalid_warning_days',
  invalid_numeric_field: 'items.reason.invalid_numeric_field',
} as const satisfies Record<string, MessageKey>

/** Localized message for any error from the item endpoints. */
export function describeItemError(e: unknown): string {
  if (e instanceof ApiError && e.reason) {
    const key = Object.prototype.hasOwnProperty.call(REASON_KEYS, e.reason)
      ? REASON_KEYS[e.reason as keyof typeof REASON_KEYS]
      : null
    return key ? t(key) : t('items.reason.unknown', { reason: e.reason })
  }
  if (e instanceof ApiError && e.status === 400) {
    return errorKeyForCode(e.code) ? describeError(e) : t('items.err.rejected')
  }
  return describeError(e)
}

export { WEEKDAYS }
