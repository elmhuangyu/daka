import { describe, expect, it } from 'vitest'
import { ApiError, type ItemDto } from './api'
import {
  allowedPolarities,
  allowedSchedules,
  buildCreateBody,
  buildEditPlan,
  describeItemError,
  emptyForm,
  formFromItem,
  holidayAllowed,
  normalizeForm,
  previewRule,
  validateForm,
} from './itemForm'

function item(over: Partial<ItemDto> = {}): ItemDto {
  return {
    id: 1,
    name: 'Run',
    description: '',
    remind_time: null,
    tracker_type: 'boolean',
    kind: 'habit',
    polarity: 'positive',
    category_id: null,
    is_sensitive: false,
    effective_sensitive: false,
    options: [],
    current_rule: {
      schedule_type: 'fixed_rrule',
      rrule_string: 'FREQ=WEEKLY;BYDAY=MO,WE',
      dtstart: '2026-01-05',
      until: null,
      holiday_mode: 'none',
      effective_from: '2026-01-05',
      option_targets: {},
    },
    ...over,
  }
}

const opt = (id: number, name: string) => ({
  id,
  name,
  unit: '',
  step: 0,
  min_val: null,
  max_val: null,
  aggregate: '',
  sort_order: id,
  target_value: null,
})

describe('matrix', () => {
  it('limits polarity and schedules like the backend', () => {
    expect(allowedPolarities('counter')).toEqual(['positive'])
    expect(allowedPolarities('metric')).toContain('negative')
    expect(allowedSchedules('boolean', 'negative')).toEqual(['quota', 'none'])
    expect(allowedSchedules('counter', 'positive')).not.toContain('rolling')
    expect(allowedSchedules('maintenance', 'positive')).not.toContain('quota')
  })
  it('holiday policy only for fixed rrule', () => {
    const f = emptyForm()
    expect(holidayAllowed(f)).toBe(true)
    f.scheduleType = 'quota'
    expect(holidayAllowed(f)).toBe(false)
  })
  it('normalizeForm repairs invalid combinations', () => {
    const f = emptyForm()
    f.polarity = 'negative'
    f.trackerType = 'counter'
    normalizeForm(f)
    expect(f.polarity).toBe('positive')
    const g = emptyForm()
    g.polarity = 'negative'
    normalizeForm(g)
    expect(g.scheduleType).toBe('quota')
    expect(g.quotaComparator).toBe('at_most')
    expect(g.quotaMeasure).toBe('occurrences')
  })
})

describe('buildCreateBody', () => {
  it('weekly rrule with holiday mode', () => {
    const f = emptyForm()
    f.name = ' Run '
    f.periodKind = 'weekly'
    f.weekDays = ['WE', 'MO']
    f.holidayMode = 'workdays_only'
    const b = buildCreateBody(f) as { name: string; rule: Record<string, unknown> }
    expect(b.name).toBe('Run')
    expect(b.rule).toEqual({
      schedule_type: 'fixed_rrule',
      rrule_string: 'FREQ=WEEKLY;BYDAY=MO,WE',
      holiday_mode: 'workdays_only',
    })
  })
  it('quota carries no rrule or holiday fields', () => {
    const f = emptyForm()
    f.scheduleType = 'quota'
    const rule = (buildCreateBody(f) as { rule: Record<string, unknown> }).rule
    expect(rule).toEqual({
      schedule_type: 'quota',
      quota_comparator: 'at_least',
      quota_count: 3,
      quota_period: 'week',
      quota_window: 'calendar',
      quota_measure: 'days',
    })
  })
  it('unscheduled and rolling', () => {
    const f = emptyForm()
    f.scheduleType = 'none'
    expect((buildCreateBody(f) as { rule: object }).rule).toEqual({ schedule_type: 'none' })
    f.scheduleType = 'rolling'
    expect((buildCreateBody(f) as { rule: object }).rule).toEqual({
      schedule_type: 'rolling',
      rolling_interval: 30,
      rolling_unit: 'day',
    })
  })
  it('event with one-time once period sets dtstart, count=1, and until', () => {
    const f = emptyForm()
    f.name = 'Tax filing'
    f.kind = 'event'
    f.periodKind = 'once'
    f.dtstart = '2026-11-15'
    const b = buildCreateBody(f) as { name: string; kind: string; rule: Record<string, unknown> }
    expect(b.name).toBe('Tax filing')
    expect(b.kind).toBe('event')
    expect(b.rule).toEqual({
      schedule_type: 'fixed_rrule',
      rrule_string: 'FREQ=DAILY;COUNT=1',
      holiday_mode: 'none',
      dtstart: '2026-11-15',
      until: '2026-11-15',
    })
  })
  it('counter with options sends per-option targets and quota option index', () => {
    const f = emptyForm()
    f.trackerType = 'counter'
    f.unit = 'ml'
    f.options = [
      { name: 'A', unit: 'ml', step: '', target: '5' },
      { name: 'B', unit: 'g', step: '2', target: '' },
    ]
    f.scheduleType = 'quota'
    f.quotaMeasure = 'sum'
    f.quotaOption = 1
    f.completionRule = 'at_least_n'
    f.completionN = '1'
    const b = buildCreateBody(f) as {
      options: Record<string, unknown>[]
      rule: Record<string, unknown>
    }
    expect(b.options[0].target_value).toBe(5)
    expect(b.options[1]).not.toHaveProperty('target_value')
    expect(b.rule.quota_option_index).toBe(1)
    expect(b.rule).not.toHaveProperty('target_value')
    expect(b.rule.completion_n).toBe(1)
  })
})

describe('validateForm', () => {
  it('requires name and a weekday, and a quota option when units differ', () => {
    const f = emptyForm()
    f.periodKind = 'weekly'
    f.weekDays = []
    expect(validateForm(f)).toEqual(['Name is required', 'Select at least one weekday'])
    const g = emptyForm()
    g.name = 'x'
    g.trackerType = 'counter'
    g.scheduleType = 'quota'
    g.quotaMeasure = 'sum'
    g.options = [
      { name: 'a', unit: 'ml', step: '', target: '' },
      { name: 'b', unit: 'g', step: '', target: '' },
    ]
    expect(validateForm(g).join()).toContain('Choose which option')
  })
})

describe('edit plan', () => {
  it('unchanged form sends nothing', () => {
    const it = item()
    const p = buildEditPlan(it, formFromItem(it), '', '2026-11-08')
    expect(p.hasPatch).toBe(false)
    expect(p.ruleChanged).toBe(false)
  })
  it('name-only change makes no rule version', () => {
    const it = item()
    const f = formFromItem(it)
    f.name = 'Jog'
    expect(buildEditPlan(it, f, '', '2026-11-08').patch).toEqual({ name: 'Jog' })
  })
  it('rrule change creates a rule patch restarting dtstart; never sends immutable fields', () => {
    const it = item()
    const f = formFromItem(it)
    f.weekDays = ['FR']
    const p = buildEditPlan(it, f, '', '2026-11-08')
    expect(p.ruleChanged).toBe(true)
    expect(p.patch).not.toHaveProperty('tracker_type')
    expect(p.patch.rule).toMatchObject({
      rrule_string: 'FREQ=WEEKLY;BYDAY=FR',
      dtstart: '2026-11-08',
      until: '',
    })
  })
  it('retroactive date forces a version with effective_from', () => {
    const it = item()
    const p = buildEditPlan(it, formFromItem(it), '2026-11-01', '2026-11-08')
    expect(p.patch.rule).toMatchObject({ effective_from: '2026-11-01' })
  })
  it('diffs options', () => {
    const it = item({ tracker_type: 'counter', options: [opt(7, 'A'), opt(8, 'B')] })
    const f = formFromItem(it)
    f.options[0].name = 'A2'
    f.options.splice(1, 1)
    f.options.push({ name: 'C', unit: '', step: '', target: '' })
    const p = buildEditPlan(it, f, '', '2026-11-08')
    expect(p.updateOptions).toEqual([0])
    expect(p.addOptions).toEqual([1])
    expect(p.removeOptionIds).toEqual([8])
  })
})

describe('formFromItem', () => {
  it('maps a weekly rule back to the builder and unknown rules to custom', () => {
    expect(formFromItem(item()).periodKind).toBe('weekly')
    const it = item()
    it.current_rule.rrule_string = 'FREQ=MONTHLY;BYDAY=MO;BYSETPOS=1'
    const f = formFromItem(it)
    expect(f.periodKind).toBe('custom')
    expect(f.custom).toBe('FREQ=MONTHLY;BYDAY=MO;BYSETPOS=1')
  })
})

describe('previewRule', () => {
  it('lists upcoming dates and sentence', () => {
    const f = emptyForm()
    f.periodKind = 'weekly'
    f.weekDays = ['MO']
    const p = previewRule(f, '2026-11-08')
    expect(p.sentence).toBe('Every week on Monday.')
    expect(p.dates?.[0]).toBe('2026-11-09')
  })
  it('describes quota and rolling', () => {
    const f = emptyForm()
    f.scheduleType = 'quota'
    expect(previewRule(f, '2026-11-08').sentence).toBe('At least 3 day(s) per calendar week.')
    f.scheduleType = 'rolling'
    expect(previewRule(f, '2026-11-08').sentence).toBe(
      'Due again 30 days after the last completion.',
    )
  })
})

describe('describeItemError', () => {
  it('maps reason codes to English', () => {
    const e = new ApiError('invalid combination', 400, 'invalid combination')
    e.reason = 'quota_option_required'
    expect(describeItemError(e)).toContain('Choose which option')
    e.reason = 'something_new'
    expect(describeItemError(e)).toContain('something_new')
    expect(describeItemError(new ApiError('name is required', 400, 'name is required'))).toBe(
      'Name is required',
    )
  })
})
